package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	"github.com/mysayasan/kopiv2/domain/notification"
)

// flow_runtime.go compiles a flow graph into an executable form and runs it against the live
// telemetry stream. It is the moving half of the engine; flows.go is the stored/validated half.
//
// Execution is single-threaded per flow BY DESIGN: one goja runtime is not goroutine-safe, and a
// flow's whole graph runs on the one worker goroutine, so a flow's sandbox is never touched
// concurrently. The worker drains an input-event channel and reconciles compiled flows against the
// database on a ticker + on an explicit change signal — the same reconcile pattern the Modbus
// poller uses, so enabling/editing/disabling a flow takes effect with no process restart.

const (
	// flowScriptTimeout fences every JS call. A script that exceeds it is interrupted and its node
	// fails; the rest of the flow is unaffected.
	flowScriptTimeout = 100 * time.Millisecond
	// flowMaxSteps bounds one event's propagation, so a wide fan-out cannot become an unbounded
	// amount of work per reading. Cycles are already rejected at save; this is defence in depth.
	flowMaxSteps = 1000
	// flowEventQueue is the ingest→runtime buffer. If it overflows (a burst faster than the worker
	// drains) the OLDEST-style backpressure is to DROP the newest event and count it — a flow is a
	// convenience layer over telemetry, never the system of record, so shedding load here is safe.
	flowEventQueue = 4096
	// flowEventBudget bounds what ONE reading may cost the worker, across every node it touches.
	//
	// flowScriptTimeout fences a single script; nothing used to fence the event. A graph is
	// allowed 200 nodes and a propagation is allowed flowMaxSteps of them, so a graph whose
	// scripts each finish just inside their budget — none of them misbehaving by any rule the
	// runtime could see — could legally spend a hundred seconds of the shared worker on ONE
	// sample. Measured on a live appliance: a 190-node graph of 80ms scripts held every other
	// flow in the install for fifteen seconds per reading. One second is far more than a
	// data-flow graph needs and far less than an operator would call an outage.
	flowEventBudget = time.Second
	// flowQuarantineAfter is how many CONSECUTIVE script timeouts a flow gets before the runtime
	// stops running it and tells somebody.
	//
	// A script that throws is ordinary — a payload arrived in a shape it did not expect — and it
	// costs nothing, so it is not counted here. A script that never RETURNS costs the whole
	// per-script budget out of a worker shared by every flow in the install, on every message,
	// forever. Five in a row is not a bad reading; it is a broken flow, and running it another ten
	// thousand times helps nobody. The count is kept per NODE and a node's own successful run
	// resets its own count; editing the flow recompiles it and clears the quarantine.
	flowQuarantineAfter = 5
)

// flowNotifier and deviceResolver are the two collaborators an OUTPUT node needs, as interfaces so the
// runtime is unit-testable with fakes (the production types *notification.Service and *DeviceService
// satisfy them).
type flowNotifier interface {
	Publish(ctx context.Context, n notification.Notification) notification.Notification
}

type deviceResolver interface {
	GetByKey(ctx context.Context, key string) (*entities.IotDevice, error)
}

// readingSink is the storage side a derived_metric node writes to (the ReadingWriter satisfies it).
type readingSink interface {
	Enqueue(r entities.DeviceReading)
}

// mqttPublish is the broker publish seam an mqtt_out node uses (Broker.Publish satisfies it).
//
// Note what that seam IS: the server's own broker handle, which answers to no ACL. It can publish
// anywhere, including a topic one of this hub's own devices is waiting for a command on — which is
// why an mqtt_out node is checked against topicGuard before it is allowed to use it.
type mqttPublish func(topic string, payload []byte, retain bool, qos byte) error

// topicGuard tells the runtime whether a topic is one a real device would act on as a COMMAND.
// *CommandService satisfies it; it is an interface so the runtime stays unit-testable with a fake.
type topicGuard interface {
	ReservedTopic(ctx context.Context, topic string) (deviceKey, command string, reserved bool)
	RecordOffPathRefusal(ctx context.Context, deviceKey, command, topic, actorName string)
}

// flowDeps are what a node's OUTPUTS reach for. A transform node touches none of it.
type flowDeps struct {
	issuer       commandIssuer // the ONE guarded actuation entry point (CommandService)
	flowNotifier flowNotifier
	devices      deviceResolver
	writer       readingSink
	publish      mqttPublish
	// topics reserves the device command topics to the guarded path. A nil guard leaves mqtt_out
	// unrestricted, which is right for a unit test that has no devices and wrong for production —
	// so app.go passes the CommandService, and the actuation bench proves it did.
	topics topicGuard
	logf   func(format string, args ...any)
}

// --- compiled form --------------------------------------------------------------------------

type boundInput struct {
	cf     *compiledFlow
	nodeId string
}

type compiledFlow struct {
	id       int64
	name     string
	sig      string // the raw graph JSON; unchanged sig on reconcile means "keep, don't rebuild"
	sandbox  *jsSandbox
	nodes    map[string]*flowNode
	outWires map[string][]string // nodeId -> downstream nodeIds
	inputs   []flowInputBinding
	deadband map[string]float64 // nodeId -> last emitted value (deadband state)
	lastPass map[string]int64   // nodeId -> last pass unix-milli (throttle state)
	debug    *debugRing
	deps     *flowDeps

	// Quarantine state. `timeouts` is touched only by the worker goroutine, which is the only
	// thing that executes a flow. `quarantined` is also READ by the status endpoint on an HTTP
	// goroutine, so it is atomic rather than plain.
	//
	// The count is PER NODE, and that is not a detail. A flow-wide counter reset by any
	// successful script would never fire on the commonest shape of all — a healthy transform
	// wired into a runaway one — because the healthy node zeroes the count on every event
	// moments before the runaway node increments it back to one.
	timeouts    map[string]int // nodeId -> consecutive script timeouts
	quarantined atomic.Bool
}

// flowRun is what one event carries as it propagates: how many nodes it has been through, and
// when its budget runs out. Both bounds belong to the EVENT, not to a node — the thing being
// protected is the worker every other flow in the install is waiting on.
type flowRun struct {
	steps    int
	deadline time.Time
}

type flowInputBinding struct {
	nodeId    string
	deviceKey string
	key       string
}

// compileFlow parses + validates the graph, compiles every code-bearing node into the sandbox, and
// indexes inputs and wires. A compile error means the flow is skipped by the runtime (logged), never
// that the process fails.
func compileFlow(id int64, name, rawGraph string, deps *flowDeps) (*compiledFlow, error) {
	g, err := parseGraph(rawGraph)
	if err != nil {
		return nil, err
	}
	cf := &compiledFlow{
		id: id, name: name, sig: rawGraph,
		sandbox:  newJSSandbox(),
		nodes:    make(map[string]*flowNode, len(g.Nodes)),
		outWires: map[string][]string{},
		deadband: map[string]float64{},
		lastPass: map[string]int64{},
		timeouts: map[string]int{},
		debug:    newDebugRing(),
		deps:     deps,
	}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		cf.nodes[n.Id] = n
		if body, ok := nodeScript(n); ok {
			// Same source the save path compiles (flows.go nodeScript) — one definition, so a
			// flow that validated cannot fail here for a reason the author was never shown.
			if err := cf.sandbox.compile(n.Id, body); err != nil {
				return nil, err
			}
		}
		switch n.Type {
		case nodeDeviceTelemetry:
			cf.inputs = append(cf.inputs, flowInputBinding{
				nodeId:    n.Id,
				deviceKey: cfgString(n.Config, "deviceKey"),
				key:       cfgString(n.Config, "key"),
			})
		}
	}
	for _, w := range g.Wires {
		cf.outWires[w.From.Node] = append(cf.outWires[w.From.Node], w.To.Node)
	}
	return cf, nil
}

// onInput seeds a message at an input node and propagates it through the graph.
func (cf *compiledFlow) onInput(ctx context.Context, nodeId string, msg *flowMessage) {
	if cf.quarantined.Load() {
		return
	}
	run := &flowRun{deadline: time.Now().Add(flowEventBudget)}
	cf.visit(ctx, nodeId, msg, run)
}

func (cf *compiledFlow) visit(ctx context.Context, nodeId string, in *flowMessage, run *flowRun) {
	if run.steps++; run.steps > flowMaxSteps {
		cf.deps.logf("flow %q exceeded %d propagation steps — stopping this event", cf.name, flowMaxSteps)
		return
	}
	if time.Now().After(run.deadline) {
		// The worker is shared. A graph that wants more than this per reading is starving every
		// other flow in the install, so the event stops here and the operator gets told which
		// flow to look at rather than a system that is mysteriously behind.
		cf.deps.logf("flow %q spent its %s budget on one reading at node %q — stopping this event; "+
			"the graph is too slow to run on every sample", cf.name, flowEventBudget, nodeId)
		return
	}
	cf.debug.record(nodeId, in) // what this node received — the inspector shows this
	node := cf.nodes[nodeId]
	if node == nil {
		return
	}
	out, emit := cf.exec(ctx, node, in)
	if !emit || out == nil {
		return
	}
	for _, next := range cf.outWires[nodeId] {
		cf.visit(ctx, next, out.clone(), run)
	}
}

// exec runs one node. It returns the message to pass on and whether to pass it. A returned
// (nil, false) drops the branch — a threshold not met, a deadband within tolerance, a script that
// returned null, or any sink (debug/notify/command).
func (cf *compiledFlow) exec(ctx context.Context, node *flowNode, in *flowMessage) (*flowMessage, bool) {
	switch node.Type {
	case nodeDeviceTelemetry:
		return in, true // a source just emits what arrived

	case nodeScale:
		n, ok := in.num()
		if !ok {
			return nil, false
		}
		factor := 1.0
		if f, ok := cfgFloat(node.Config, "factor"); ok {
			factor = f
		}
		offset, _ := cfgFloat(node.Config, "offset")
		out := in.clone()
		out.Payload = n*factor + offset
		return out, true

	case nodeThreshold:
		n, ok := in.num()
		if !ok {
			return nil, false
		}
		val, _ := cfgFloat(node.Config, "value")
		if compareOp(n, cfgString(node.Config, "op"), val) {
			return in, true
		}
		return nil, false

	case nodeDeadband:
		n, ok := in.num()
		if !ok {
			return nil, false
		}
		delta, _ := cfgFloat(node.Config, "delta")
		if last, seen := cf.deadband[node.Id]; seen && math.Abs(n-last) < delta {
			return nil, false
		}
		cf.deadband[node.Id] = n
		return in, true

	case nodeThrottle:
		// Rate-limit a branch: pass at most once per `seconds`, dropping anything that arrives
		// inside the window. Stateful but timer-free — it never DEFERS a message, it only drops,
		// so it cannot fire after the fact and cannot loop.
		secs, _ := cfgFloat(node.Config, "seconds")
		now := time.Now().UnixMilli()
		if last, seen := cf.lastPass[node.Id]; seen && float64(now-last) < secs*1000 {
			return nil, false
		}
		cf.lastPass[node.Id] = now
		return in, true

	case nodeFunction, nodeExpression, nodeSwitch:
		out, err := cf.sandbox.run(node.Id, in, flowScriptTimeout)
		if err != nil {
			cf.deps.logf("flow %q node %q script error: %v", cf.name, node.Id, err)
			cf.noteScriptError(ctx, node.Id, err)
			return nil, false
		}
		delete(cf.timeouts, node.Id) // this script returned; it is not the wedged one
		if out == nil {
			return nil, false
		}
		return out, true

	case nodeDebug:
		return nil, false // a sink; the message was already recorded on entry

	case nodeNotify:
		cf.doNotify(ctx, node, in)
		return nil, false

	case nodeCommand:
		cf.doCommand(ctx, node, in)
		return nil, false

	case nodeDerivedMetric:
		cf.doDerived(ctx, node, in)
		return nil, false

	case nodeMqttOut:
		cf.doMqttOut(ctx, node, in)
		return nil, false

	default:
		// Unreachable — parseGraph refuses unknown types — but closed by default all the same.
		cf.deps.logf("flow %q has unhandled node type %q", cf.name, node.Type)
		return nil, false
	}
}

// noteScriptError counts consecutive TIMEOUTS and quarantines a flow that keeps hitting them.
//
// The distinction between a timeout and a thrown error is the whole point. A script that throws
// has met a payload it did not expect; that is ordinary, it costs nothing, and quarantining a
// flow for it would take an alerting flow off the air because one sensor sent a null. A script
// that does not RETURN spends the entire per-script budget out of a worker every other flow is
// queued behind, and it will do so on every message until somebody notices — which, before this,
// nobody ever did: the only trace was an INFO log line per event.
func (cf *compiledFlow) noteScriptError(ctx context.Context, nodeId string, err error) {
	if !isScriptTimeout(err) {
		return
	}
	cf.timeouts[nodeId]++
	n := cf.timeouts[nodeId]
	if n < flowQuarantineAfter || cf.quarantined.Load() {
		return
	}
	cf.quarantined.Store(true)
	cf.deps.logf("flow %q quarantined: node %q exceeded its time budget %d times in a row; "+
		"the flow has been stopped until it is edited", cf.name, nodeId, n)
	if cf.deps.flowNotifier == nil {
		return
	}
	cf.deps.flowNotifier.Publish(ctx, notification.Notification{
		Category: notification.CategorySystem,
		Severity: notification.Warning,
		Title:    "Flow stopped: " + cf.name,
		Body: fmt.Sprintf("The script on node %q in flow %q did not finish within its time budget "+
			"%d times in a row, so the flow has been stopped to keep it from delaying every other "+
			"flow. Edit and save the flow to start it again.", nodeId, cf.name, n),
		Source: "flow:" + cf.name,
		Data:   map[string]any{"flowId": cf.id, "node": nodeId, "reason": "quarantined"},
	})
}

// doNotify publishes a notification. A flow's notification is a first-class alert, indistinguishable
// from a rule's except for its Source ("flow:<name>").
func (cf *compiledFlow) doNotify(ctx context.Context, node *flowNode, in *flowMessage) {
	if cf.deps.flowNotifier == nil {
		return
	}
	category := notification.CategoryDeviceAlert
	if cfgString(node.Config, "category") == "system" {
		category = notification.CategorySystem
	}
	title := cfgString(node.Config, "title")
	if title == "" {
		title = cf.name
	}
	body := cfgString(node.Config, "body")
	if body == "" {
		body = fmt.Sprintf("%s = %v", in.Key, in.Payload)
	}
	cf.deps.flowNotifier.Publish(ctx, notification.Notification{
		Category: category,
		Severity: severityOf(cfgString(node.Config, "severity")),
		Title:    title,
		Body:     body,
		Source:   "flow:" + cf.name,
		Data: map[string]any{
			"flowId":    cf.id,
			"node":      node.Id,
			"key":       in.Key,
			"deviceKey": in.DeviceKey,
			"value":     in.Payload,
		},
	})
}

// doCommand actuates — the ONLY node type that can. It resolves the target device by natural key
// and issues through CommandService.Issue, which re-applies every gate (actuation-enabled, admin
// intent, declared bounds, rate limit, audit, never auto-retried). The flow is recorded as the
// actor ("flow:<name>", id 0), so the audit trail shows exactly what commanded the device.
func (cf *compiledFlow) doCommand(ctx context.Context, node *flowNode, in *flowMessage) {
	if cf.deps.issuer == nil || cf.deps.devices == nil {
		return
	}
	deviceKey := cfgString(node.Config, "deviceKey")
	command := cfgString(node.Config, "command")
	if deviceKey == "" || command == "" {
		cf.deps.logf("flow %q command node %q missing deviceKey/command", cf.name, node.Id)
		return
	}
	// The value is an explicit config value if given, otherwise the message payload.
	value, ok := cfgFloat(node.Config, "value")
	if !ok {
		value, _ = in.num()
	}
	dev, err := cf.deps.devices.GetByKey(ctx, deviceKey)
	if err != nil || dev == nil {
		cf.deps.logf("flow %q command node %q: device %q not found", cf.name, node.Id, deviceKey)
		return
	}
	if _, err := cf.deps.issuer.Issue(ctx, dev.Id, IssueRequest{Name: command, Value: value}, 0, "flow:"+cf.name); err != nil {
		cf.deps.logf("flow %q command to %q refused/failed: %v", cf.name, deviceKey, err)
	}
}

// doDerived persists a computed value as a telemetry reading — the "derived metric" of the Layer B
// design (net grid, self-consumption, battery autonomy). It writes straight to the reading store
// under a target device's namespace, so the value is stored, rolled up and charted like any other
// reading. It deliberately does NOT re-enter the ingest pipeline: a derived write that fed back
// through the flow tap could loop, and a flow that wants to alert on its derived value has a
// threshold→notify branch for exactly that. The target device must exist; the key is a new
// synthetic series on it.
func (cf *compiledFlow) doDerived(ctx context.Context, node *flowNode, in *flowMessage) {
	if cf.deps.writer == nil || cf.deps.devices == nil {
		return
	}
	deviceKey := cfgString(node.Config, "deviceKey")
	key := cfgString(node.Config, "key")
	if deviceKey == "" || key == "" {
		cf.deps.logf("flow %q derived node %q missing deviceKey/key", cf.name, node.Id)
		return
	}
	value, ok := in.num()
	if !ok {
		return // a derived metric is numeric by definition
	}
	dev, err := cf.deps.devices.GetByKey(ctx, deviceKey)
	if err != nil || dev == nil {
		cf.deps.logf("flow %q derived node %q: device %q not found", cf.name, node.Id, deviceKey)
		return
	}
	cf.deps.writer.Enqueue(entities.DeviceReading{DeviceId: dev.Id, Key: key, Ts: time.Now().UnixMilli(), Num: value})
}

// doMqttOut publishes the message payload to an MQTT topic — the bridge OUT of the hub (feed a
// processed value to another system, or drive a home-automation subscriber). It publishes DATA.
//
// It may not publish a COMMAND. The topics this hub's own devices act on are reserved to the
// guarded path, and an mqtt_out node aimed at one is refused and written down. That is not a
// nicety: this node publishes through the server's own broker handle, which is subject to no ACL,
// so without the check it would move a relay whose actuation is switched off, with a value outside
// the declared safe range, past the duty-cycle limit, leaving nothing in the trail — every gate in
// CommandService.Issue bypassed at once by a node whose stated job is to bridge a value out.
//
// A command output node is the way to actuate a myiotsan device, and it is the only way.
func (cf *compiledFlow) doMqttOut(ctx context.Context, node *flowNode, in *flowMessage) {
	if cf.deps.publish == nil {
		return
	}
	topic := cfgString(node.Config, "topic")
	if topic == "" {
		cf.deps.logf("flow %q mqtt node %q missing topic", cf.name, node.Id)
		return
	}
	if cf.deps.topics != nil {
		if devKey, command, reserved := cf.deps.topics.ReservedTopic(ctx, topic); reserved {
			cf.deps.topics.RecordOffPathRefusal(ctx, devKey, command, topic, "flow:"+cf.name)
			return
		}
	}
	qos := byte(0)
	if q, ok := cfgFloat(node.Config, "qos"); ok {
		qos = byte(q)
	}
	if err := cf.deps.publish(topic, payloadBytes(in.Payload), cfgBool(node.Config, "retain"), qos); err != nil {
		cf.deps.logf("flow %q mqtt publish to %q failed: %v", cf.name, topic, err)
	}
}

// payloadBytes renders a message payload for the wire: a number as its shortest decimal, a string
// as-is, anything else as JSON.
func payloadBytes(v any) []byte {
	switch p := v.(type) {
	case string:
		return []byte(p)
	case float64:
		return []byte(strconv.FormatFloat(p, 'f', -1, 64))
	default:
		if b, err := json.Marshal(v); err == nil {
			return b
		}
		return []byte(fmt.Sprintf("%v", v))
	}
}

func compareOp(a float64, op string, b float64) bool {
	switch strings.TrimSpace(op) {
	case ">":
		return a > b
	case ">=":
		return a >= b
	case "<":
		return a < b
	case "<=":
		return a <= b
	case "==":
		return a == b
	case "!=":
		return a != b
	default:
		return false
	}
}

// --- debug ring (inspector) -----------------------------------------------------------------

// debugRing keeps the latest message seen at each node plus an execution count, for the live
// inspector. It is the one structure a non-worker goroutine (the HTTP debug endpoint) reads, so it
// carries its own lock.
type debugRing struct {
	mu    sync.Mutex
	last  map[string]debugEntry
	count int64
}

type debugEntry struct {
	Payload any    `json:"payload"`
	Key     string `json:"key"`
	Ts      int64  `json:"ts"`
	Seq     int64  `json:"seq"`
}

func newDebugRing() *debugRing { return &debugRing{last: map[string]debugEntry{}} }

func (d *debugRing) record(nodeId string, m *flowMessage) {
	d.mu.Lock()
	d.count++
	d.last[nodeId] = debugEntry{Payload: m.Payload, Key: m.Key, Ts: m.Ts, Seq: d.count}
	d.mu.Unlock()
}

func (d *debugRing) snapshot() map[string]debugEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]debugEntry, len(d.last))
	for k, v := range d.last {
		out[k] = v
	}
	return out
}

// --- the runtime ----------------------------------------------------------------------------

type flowEvent struct {
	deviceKey string
	key       string
	value     float64
	ts        int64
}

// FlowRuntime drains telemetry events and runs the compiled flows. Its compiled/index state is
// touched by the worker goroutine (reconcile + dispatch) and, read-only, by the debug endpoint —
// guarded by mu. Actual JS execution only ever happens on the worker goroutine.
type FlowRuntime struct {
	svc      *FlowService
	deps     *flowDeps
	events   chan flowEvent
	reloadCh chan struct{}

	mu       sync.RWMutex
	compiled map[int64]*compiledFlow
	index    map[string][]boundInput // deviceKey\x00key -> inputs to seed
	// broken records the flows the runtime could NOT compile, by id, with the reason. An enabled
	// flow that does not compile used to leave one INFO log line and nothing else — no state on
	// the row, nothing in the feed — so the canvas showed an enabled flow that was never going to
	// run. This is what FlowState reads and what the flows list renders.
	broken map[int64]string
	// told remembers which compile failure has already been reported, so a reconcile every thirty
	// seconds does not put the same message in the operator's feed twice a minute forever.
	told map[int64]string

	// dropped counts events shed when the queue overflowed. It is atomic because OnReading runs
	// on whatever goroutine delivered the reading (the broker hook, the Modbus poller) while the
	// stats endpoint reads it on an HTTP one.
	dropped atomic.Int64
}

func NewFlowRuntime(svc *FlowService, issuer commandIssuer, notif flowNotifier, devices deviceResolver, writer readingSink, publish mqttPublish, topics topicGuard, logf func(string, ...any)) *FlowRuntime {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &FlowRuntime{
		svc:      svc,
		deps:     &flowDeps{issuer: issuer, flowNotifier: notif, devices: devices, writer: writer, publish: publish, topics: topics, logf: logf},
		events:   make(chan flowEvent, flowEventQueue),
		reloadCh: make(chan struct{}, 1),
		compiled: map[int64]*compiledFlow{},
		index:    map[string][]boundInput{},
		broken:   map[int64]string{},
		told:     map[int64]string{},
	}
}

func indexKey(deviceKey, key string) string { return deviceKey + "\x00" + key }

// OnReading is the tap the ingest pipeline calls for every decoded sample (mirrors
// RuleService.OnReading). It only enqueues — it must never block ingest — and sheds the newest
// event if the buffer is full.
func (r *FlowRuntime) OnReading(_ context.Context, dev *entities.IotDevice, key string, value float64, nowSec int64) {
	if dev == nil {
		return
	}
	select {
	case r.events <- flowEvent{deviceKey: dev.DeviceKey, key: key, value: value, ts: nowSec}:
	default:
		n := r.dropped.Add(1)
		// Log on a ramp, not per event: under sustained overload the log becomes the bottleneck.
		// The COUNTER is the thing to watch (GET /api/flows/stats, and the Prometheus gauge) —
		// this line is only so a shed shows up in an ordinary log read as well.
		if n == 1 || n%1000 == 0 {
			r.deps.logf("flow runtime is shedding telemetry events — the flows are not keeping up "+
				"(%d dropped so far); a flow bound to a busy key is silently missing readings", n)
		}
	}
}

// SignalReload asks the worker to recompile. Non-blocking: a pending signal already covers us.
func (r *FlowRuntime) SignalReload() {
	select {
	case r.reloadCh <- struct{}{}:
	default:
	}
}

// Run is the supervised worker loop: reconcile on a ticker + on a change signal, dispatch events in
// between. It returns when ctx is cancelled (a clean stop).
func (r *FlowRuntime) Run(ctx context.Context, reconcileEvery time.Duration) {
	r.reload(ctx)
	tk := time.NewTicker(reconcileEvery)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			r.reload(ctx)
		case <-r.reloadCh:
			r.reload(ctx)
		case ev := <-r.events:
			r.dispatch(ctx, ev)
		}
	}
}

// reload reconciles compiled flows against the enabled rows. An unchanged graph is KEPT (preserving
// its sandbox's flow-context state across reconciles); only an added or edited flow is recompiled;
// a removed/disabled flow is dropped. A compile error skips that one flow, logged, and never stops
// the others.
func (r *FlowRuntime) reload(ctx context.Context) {
	rows, err := r.svc.ListEnabled(ctx)
	if err != nil {
		r.deps.logf("flow runtime: could not list flows: %v", err)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	next := make(map[int64]*compiledFlow, len(rows))
	broken := make(map[int64]string)
	for _, f := range rows {
		if existing, ok := r.compiled[f.Id]; ok && existing.sig == f.Graph {
			existing.name = f.Name
			next[f.Id] = existing
			continue
		}
		cf, err := compileFlow(f.Id, f.Name, f.Graph, r.deps)
		if err != nil {
			// A graph that cannot compile is skipped, as it always was — one bad flow may not stop
			// the others. What is new is that it stops being invisible: the state is recorded for
			// the flows list, and the operator is told once. Scripts are refused at save now, so
			// reaching here means an IMPORTED flow, or a row written by an older build — which is
			// exactly the case nobody was ever going to notice.
			r.deps.logf("flow %q (id %d) did not compile — skipped: %v", f.Name, f.Id, err)
			broken[f.Id] = err.Error()
			r.reportBroken(ctx, f.Id, f.Name, err.Error())
			continue
		}
		next[f.Id] = cf
	}
	r.compiled = next
	r.broken = broken
	for id := range r.told {
		if _, still := broken[id]; !still {
			delete(r.told, id) // fixed or gone: a future failure is worth telling about again
		}
	}

	idx := map[string][]boundInput{}
	for _, cf := range next {
		for _, in := range cf.inputs {
			if in.deviceKey == "" || in.key == "" {
				continue
			}
			k := indexKey(in.deviceKey, in.key)
			idx[k] = append(idx[k], boundInput{cf: cf, nodeId: in.nodeId})
		}
	}
	r.index = idx
}

// reportBroken tells the operator, once, that an enabled flow cannot run. Caller holds r.mu.
func (r *FlowRuntime) reportBroken(ctx context.Context, id int64, name, detail string) {
	if r.told[id] == detail {
		return
	}
	r.told[id] = detail
	if r.deps.flowNotifier == nil {
		return
	}
	r.deps.flowNotifier.Publish(ctx, notification.Notification{
		Category: notification.CategorySystem,
		Severity: notification.Warning,
		Title:    "Flow not running: " + name,
		Body: fmt.Sprintf("Flow %q is enabled but could not be compiled, so it is not running: %s. "+
			"Open it and fix the node it names.", name, detail),
		Source: "flow:" + name,
		Data:   map[string]any{"flowId": id, "reason": "compile-error", "detail": detail},
	})
}

// FlowState is what the flows list shows about a flow the RUNTIME knows something about: it is
// enabled, and it is either running, stopped because it could not be compiled, or stopped because
// its script kept running away. Anything the runtime has nothing to say about is simply running.
type FlowState struct {
	State  string `json:"state"`            // "running" | "error" | "quarantined"
	Detail string `json:"detail,omitempty"` // why, when it is not running
}

// States returns the runtime state of every enabled flow, by id.
func (r *FlowRuntime) States() map[int64]FlowState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[int64]FlowState, len(r.compiled)+len(r.broken))
	for id, detail := range r.broken {
		out[id] = FlowState{State: "error", Detail: detail}
	}
	for id, cf := range r.compiled {
		if cf.quarantined.Load() {
			out[id] = FlowState{State: "quarantined",
				Detail: "a script exceeded its time budget repeatedly; edit and save the flow to start it again"}
			continue
		}
		out[id] = FlowState{State: "running"}
	}
	return out
}

// Stats is what the flow runtime is doing, for /api/flows/stats and the metrics sampler.
//
// `dropped` is the reason this exists. The runtime sheds events when its queue overflows, and
// until now it counted them into a field nothing ever read — the comment said "for logging" and
// no line logged it. A flow that quietly stops firing under load has no other symptom: the broker
// keeps accepting publishes, the readings keep landing, the chart keeps drawing, and only the
// automation is missing. services/metrics.go states the rule this was breaking — instrument what
// fails silently.
type FlowStats struct {
	Compiled    int   `json:"compiled"`    // enabled flows the runtime is running
	Broken      int   `json:"broken"`      // enabled flows that would not compile
	Quarantined int   `json:"quarantined"` // flows stopped for running away
	Bindings    int   `json:"bindings"`    // (device,key) pairs any flow is listening on
	Queued      int   `json:"queued"`      // events waiting for the worker
	Capacity    int   `json:"capacity"`    // how many it can hold before shedding
	Dropped     int64 `json:"dropped"`     // events SHED because the worker could not keep up
}

func (r *FlowRuntime) Stats() FlowStats {
	r.mu.RLock()
	s := FlowStats{
		Compiled: len(r.compiled),
		Broken:   len(r.broken),
		Bindings: len(r.index),
	}
	for _, cf := range r.compiled {
		if cf.quarantined.Load() {
			s.Quarantined++
		}
	}
	r.mu.RUnlock()
	s.Queued = len(r.events)
	s.Capacity = cap(r.events)
	s.Dropped = r.dropped.Load()
	return s
}

// dispatch delivers one telemetry event to every input node bound to it.
func (r *FlowRuntime) dispatch(ctx context.Context, ev flowEvent) {
	r.mu.RLock()
	binds := r.index[indexKey(ev.deviceKey, ev.key)]
	r.mu.RUnlock()
	for _, b := range binds {
		seed := &flowMessage{Payload: ev.value, Key: ev.key, DeviceKey: ev.deviceKey, Ts: ev.ts}
		b.cf.onInput(ctx, b.nodeId, seed)
	}
}

// DebugSnapshot returns the latest per-node values of a running flow, for the inspector. Empty if
// the flow is not currently compiled (disabled or never enabled).
func (r *FlowRuntime) DebugSnapshot(flowId int64) map[string]debugEntry {
	r.mu.RLock()
	cf := r.compiled[flowId]
	r.mu.RUnlock()
	if cf == nil {
		return map[string]debugEntry{}
	}
	return cf.debug.snapshot()
}

// TestRun compiles a flow OFF the worker (its own throwaway sandbox, so it never touches the live
// runtime's state or another goroutine's goja runtime) and injects a synthetic value at every input
// node, returning the resulting per-node snapshot. This backs POST /flows/{id}/run.
func (r *FlowRuntime) TestRun(ctx context.Context, flow *entities.IotFlow, seed float64) (map[string]debugEntry, error) {
	cf, err := compileFlow(flow.Id, flow.Name, flow.Graph, r.deps)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	for _, in := range cf.inputs {
		cf.onInput(ctx, in.nodeId, &flowMessage{
			Payload: seed, Key: in.key, DeviceKey: in.deviceKey, Ts: now,
		})
	}
	return cf.debug.snapshot(), nil
}
