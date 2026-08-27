package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// flow_eval.go is the JavaScript substrate for the flow engine — the ONE place goja is touched.
// It is deliberately isolated from the rest of the runtime so the sandbox can be unit-tested on
// its own (a script that computes, a script that loops forever, a script that reaches for the
// filesystem) without a graph, a broker or a database around it.
//
// Safety model, in three lines:
//   - A bare goja.Runtime has NO require, no filesystem, no network, no os, no host bindings — only
//     the ECMAScript standard library (Math, JSON, Date, String, …). We add nothing to it, so a
//     script's whole world is the `msg` it is handed and the value it returns.
//   - Every call is fenced by a watchdog that Interrupt()s the runtime after a hard timeout, so a
//     `while(true){}` cannot wedge the flow worker. The runtime is cleared and reused afterwards.
//   - A script cannot actuate. It only transforms a message; only an OUTPUT node acts, and the
//     command output routes through CommandService.Issue. This file never calls the actuation layer.

// scriptTimeoutMessage is what the watchdog interrupts a script with. The runtime distinguishes a
// TIMED-OUT script from one that merely threw: a script that throws on an odd payload is ordinary
// (and the branch is dropped), while a script that never returns costs the shared worker its whole
// budget on every message, and repeating that is what gets a flow quarantined.
const scriptTimeoutMessage = "flow: script exceeded time budget"

// isScriptTimeout reports whether an error from run() is the watchdog rather than the script.
func isScriptTimeout(err error) bool {
	if err == nil {
		return false
	}
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		return true
	}
	return strings.Contains(err.Error(), scriptTimeoutMessage)
}

// flowMessage is the unit that travels along a wire. Payload is the value most nodes care about
// (a number after decoding, but a script may set it to a string or bool); the rest is provenance a
// downstream node or the debug inspector can read. It is the Go mirror of the JS `msg` object.
type flowMessage struct {
	Payload   any            `json:"payload"`
	Key       string         `json:"key"`
	DeviceKey string         `json:"deviceKey"`
	Ts        int64          `json:"ts"`
	Meta      map[string]any `json:"meta,omitempty"`
}

func (m *flowMessage) clone() *flowMessage {
	if m == nil {
		return &flowMessage{}
	}
	cp := *m
	if m.Meta != nil {
		cp.Meta = make(map[string]any, len(m.Meta))
		for k, v := range m.Meta {
			cp.Meta[k] = v
		}
	}
	return &cp
}

// num returns the payload as a float64 when it is numeric. The threshold/scale/aggregate nodes and
// the deadband all speak numbers; a non-numeric payload simply is not one of them.
func (m *flowMessage) num() (float64, bool) { return coerceFloat(m.Payload) }

func (m *flowMessage) toJS() map[string]any {
	return map[string]any{
		"payload":   m.Payload,
		"key":       m.Key,
		"deviceKey": m.DeviceKey,
		"ts":        m.Ts,
		"meta":      m.Meta,
	}
}

// jsSandbox is one goja runtime plus the compiled scripts of every code-bearing node in a single
// flow. goja runtimes are NOT goroutine-safe, which is fine and by design: a flow's whole graph
// executes on one goroutine (the flow worker), so one sandbox per flow never races.
type jsSandbox struct {
	rt  *goja.Runtime
	fns map[string]goja.Callable // nodeID -> compiled function(msg)
}

// flowContextBootstrap installs a persistent per-flow scratchpad, `flow`, with get/set — the
// idiom Node-RED uses and the one thing our data-flow model needs to combine streams: a node on
// the grid-export wire stashes the latest export, the node on the grid-import wire reads both and
// computes net grid. It lives on the runtime, so it persists across messages and across the flow's
// nodes (the runtime is one-per-flow). It is pure in-memory JS — no host binding, nothing to escape
// through.
const flowContextBootstrap = `var __flowctx = {};
var flow = {
  get: function(k){ return __flowctx[k]; },
  set: function(k, v){ __flowctx[k] = v; return v; }
};`

func newJSSandbox() *jsSandbox {
	rt := goja.New()
	// Best-effort: the bootstrap is a constant we control, so it cannot fail in practice.
	_, _ = rt.RunString(flowContextBootstrap)
	return &jsSandbox{rt: rt, fns: map[string]goja.Callable{}}
}

// compile wraps a user body in `(function(msg){ … })`, evaluates it to a callable, and caches it
// under the node id. A compile error here is an authoring error surfaced at save time, never a
// runtime surprise.
func (s *jsSandbox) compile(nodeID, body string) error {
	prog, err := goja.Compile("flow:"+nodeID, "(function(msg){\n"+body+"\n})", false)
	if err != nil {
		return fmt.Errorf("compile node %s: %w", nodeID, err)
	}
	v, err := s.rt.RunProgram(prog)
	if err != nil {
		return fmt.Errorf("load node %s: %w", nodeID, err)
	}
	fn, ok := goja.AssertFunction(v)
	if !ok {
		return fmt.Errorf("node %s did not compile to a function", nodeID)
	}
	s.fns[nodeID] = fn
	return nil
}

// run executes a node's script against a message under a watchdog. It returns:
//   - (out, nil)  — the transformed message to pass downstream,
//   - (nil, nil)  — the script returned null/undefined: DROP the message (Node-RED semantics),
//   - (nil, err)  — a script error or a timeout: the node failed; the caller records it and the
//     rest of the flow is unaffected (partial failure is first-class, like a scene action).
func (s *jsSandbox) run(nodeID string, in *flowMessage, timeout time.Duration) (*flowMessage, error) {
	fn := s.fns[nodeID]
	if fn == nil {
		return nil, fmt.Errorf("node %s has no compiled script", nodeID)
	}
	arg := s.rt.ToValue(in.toJS())

	// Start clean. The watchdog below runs on another goroutine, and timer.Stop() does not wait
	// for a func that has already begun — so a script finishing in the same instant the watchdog
	// fires can leave an Interrupt pending on the runtime AFTER this call has cleared it. Nothing
	// notices until the next message, which is then killed for a budget it never came close to
	// using: one silently lost reading, on a flow that is working perfectly. Clearing on the way
	// IN as well as on the way out closes that window — a pending interrupt can only ever belong
	// to a call that has already finished.
	s.rt.ClearInterrupt()

	// Watchdog: Interrupt is meant to be called from another goroutine — that is exactly what it
	// is for. If the script finishes first, Stop() cancels the timer before it can fire.
	timer := time.AfterFunc(timeout, func() { s.rt.Interrupt(scriptTimeoutMessage) })
	ret, err := fn(goja.Undefined(), arg)
	timer.Stop()
	s.rt.ClearInterrupt() // leave the runtime clean for the next message even if we interrupted

	if err != nil {
		return nil, err
	}
	if ret == nil || goja.IsUndefined(ret) || goja.IsNull(ret) {
		return nil, nil // drop
	}

	switch exported := ret.Export().(type) {
	case map[string]any:
		return msgFromMap(exported, in), nil
	default:
		// A bare return (a number/string/bool) is a convenience: keep the message's provenance and
		// replace only its payload.
		out := in.clone()
		out.Payload = normalizePayload(exported)
		return out, nil
	}
}

// msgFromMap rebuilds a flowMessage from the object a script returned, falling back to the input's
// provenance for any field the script did not set.
func msgFromMap(m map[string]any, in *flowMessage) *flowMessage {
	out := in.clone()
	if v, ok := m["payload"]; ok {
		out.Payload = normalizePayload(v)
	}
	if v, ok := m["key"].(string); ok {
		out.Key = v
	}
	if v, ok := m["deviceKey"].(string); ok {
		out.DeviceKey = v
	}
	if v, ok := coerceFloat(m["ts"]); ok {
		out.Ts = int64(v)
	}
	if v, ok := m["meta"].(map[string]any); ok {
		out.Meta = v
	}
	return out
}

// normalizePayload collapses goja's numeric exports (int64 for integral results, float64 otherwise)
// to a float64, so every downstream node sees one numeric type; non-numbers pass through unchanged.
func normalizePayload(v any) any {
	if f, ok := coerceFloat(v); ok {
		return f
	}
	return v
}

// coerceFloat accepts every numeric shape goja and JSON hand us.
func coerceFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
