# Module: apps/myiotsan/services/flow_eval.go

## Purpose

The JavaScript substrate for the flow engine — the ONE place the embedded `github.com/dop251/goja`
interpreter (a pure-Go ECMAScript implementation, new dependency) is touched. Deliberately isolated
from the rest of the runtime so the sandbox can be unit-tested on its own (a script that computes, a
script that loops forever, a script that reaches for the filesystem) without a graph, a broker or a
database around it.

## Safety model

- A bare `goja.Runtime` has NO `require`, no filesystem, no network, no `os`, no host bindings —
  only the ECMAScript standard library (`Math`, `JSON`, `Date`, `String`, …). Nothing is added to
  it, so a script's whole world is the `msg` it is handed and the value it returns.
- Every call is fenced by a watchdog (`time.AfterFunc` + `goja.Runtime.Interrupt`) that interrupts
  the runtime after `flowScriptTimeout` (100ms, `flow_runtime.go.md`), so a `while(true){}` cannot
  wedge the flow worker. The runtime is cleared (`ClearInterrupt`) both on the way IN to `run` and
  on the way out, so a watchdog that fires just as its script returns cannot leave a pending
  interrupt that kills the NEXT, unrelated call.
- A script cannot actuate. It only transforms a message; only an OUTPUT node acts, and the command
  output routes through `CommandService.Issue`. This file never calls the actuation layer.

## Key Type: flowMessage

```go
type flowMessage struct {
    Payload   any            // the value most nodes care about (a number after decoding, or a string/bool a script set)
    Key       string
    DeviceKey string
    Ts        int64
    Meta      map[string]any
}
func (m *flowMessage) clone() *flowMessage
func (m *flowMessage) num() (float64, bool)     // threshold/scale/deadband all speak numbers
func (m *flowMessage) toJS() map[string]any     // the Go mirror of the JS `msg` object
```

The unit that travels along a wire, mirrored into/out of JS as a plain object.

## Key Type: jsSandbox

```go
type jsSandbox struct {
    rt  *goja.Runtime
    fns map[string]goja.Callable // nodeID -> compiled function(msg)
}
func newJSSandbox() *jsSandbox
func (s *jsSandbox) compile(nodeID, body string) error
func (s *jsSandbox) run(nodeID string, in *flowMessage, timeout time.Duration) (*flowMessage, error)
```

One goja runtime plus the compiled scripts of every code-bearing node in a SINGLE flow. goja
runtimes are NOT goroutine-safe, which is fine and by design: a flow's whole graph executes on one
goroutine (the flow worker), so one sandbox per flow never races.

`newJSSandbox` installs `flowContextBootstrap` — a persistent per-flow scratchpad, `flow`, with
`get`/`set` — the idiom Node-RED uses and the one thing the data-flow model needs to combine
streams: a node on the grid-export wire stashes the latest export, the node on the grid-import wire
reads both and computes net grid. It lives on the runtime, so it persists across messages and across
the flow's nodes. It is pure in-memory JS — no host binding, nothing to escape through.

`compile` wraps a user body in `(function(msg){ … })`, evaluates it to a callable, and caches it
under the node id. A compile error here is an authoring error surfaced at SAVE time, never a runtime
surprise.

`run` executes a node's script against a message under the watchdog:

- `(out, nil)` — the transformed message to pass downstream.
- `(nil, nil)` — the script returned `null`/`undefined`: DROP the message (Node-RED semantics).
- `(nil, err)` — a script error or a timeout: the node failed; the caller records it and the rest of
  the flow is unaffected (partial failure is first-class, like a scene action).

`isScriptTimeout(err)` tells a watchdog TIMEOUT (`goja.InterruptedError`, or the wrapped
`scriptTimeoutMessage`) apart from an ordinary thrown error. `flow_runtime.go.md`'s
`compiledFlow.noteScriptError` is the reason this distinction exists: a script that throws is
routine (an unexpected payload) and costs nothing, but a script that never returns spends the
watchdog's whole budget on every message, and repeating that is what gets a flow quarantined.

A bare `return` of a number/string/bool is a convenience: `run` keeps the message's provenance and
replaces only its payload (`normalizePayload`); an object return (`msgFromMap`) rebuilds a
`flowMessage`, falling back to the input's provenance for any field the script did not set.

## Notes

- `coerceFloat` accepts every numeric shape goja and JSON hand back (`float64`/`float32`/`int`/
  `int32`/`int64`), collapsing them to one type so every downstream node sees a consistent numeric
  payload.
- `flows_test.go.md`'s `TestFlow_SandboxCannotReachHost` and `TestFlow_WatchdogKillsInfiniteLoop`
  pin exactly the two guarantees this file exists to make; `TestFlow_StaleInterruptDoesNotKillTheNextScript`
  pins the clear-on-the-way-in fix — a runtime carrying a pending interrupt from a call that
  already finished must still run the next script.
