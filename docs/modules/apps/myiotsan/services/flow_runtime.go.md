# Module: apps/myiotsan/services/flow_runtime.go

## Purpose

Compiles a flow graph into an executable form and runs it against the live telemetry stream. It is
the moving half of the engine; `flows.go.md` is the stored/validated half.

Execution is single-threaded PER FLOW by design: one goja runtime is not goroutine-safe, and a
flow's whole graph runs on the one worker goroutine, so a flow's sandbox is never touched
concurrently. The worker drains an input-event channel and reconciles compiled flows against the
database on a ticker + on an explicit change signal — the same reconcile pattern the Modbus poller
uses (`modbus_poller.go.md`), so enabling/editing/disabling a flow takes effect with no process
restart.

## Key constants

```go
flowScriptTimeout = 100 * time.Millisecond // fences every JS call; interrupted script's node fails, rest of flow unaffected
flowMaxSteps      = 1000                   // bounds one event's propagation (defence in depth; cycles already rejected at save)
flowEventQueue    = 4096                   // ingest -> runtime buffer; overflow DROPS the newest event and counts it
```

A flow is a convenience layer over telemetry, never the system of record, so shedding load under
backpressure is the deliberate choice — never blocking ingest.

## Key Type: compiledFlow

```go
type compiledFlow struct {
    id, name string/int64
    sig      string // raw graph JSON; unchanged sig on reconcile means "keep, don't rebuild"
    sandbox  *jsSandbox
    nodes    map[string]*flowNode
    outWires map[string][]string
    inputs   []flowInputBinding
    deadband map[string]float64 // nodeId -> last emitted value
    lastPass map[string]int64   // nodeId -> last pass unix-milli (throttle state, P4)
    debug    *debugRing
    deps     *flowDeps
}

func compileFlow(id int64, name, rawGraph string, deps *flowDeps) (*compiledFlow, error)
func (cf *compiledFlow) onInput(ctx context.Context, nodeId string, msg *flowMessage)
func (cf *compiledFlow) exec(ctx context.Context, node *flowNode, in *flowMessage) (*flowMessage, bool)
```

`compileFlow` parses + validates the graph (`parseGraph`), compiles every code-bearing node
(`function`/`expression`/`switch`) into the sandbox, and indexes inputs and wires. A compile error
means the flow is SKIPPED by the runtime (logged), never that the process fails.

`onInput`/`visit` seed a message at an input node and propagate it depth-first through the wires,
recording each node's received message into `debug` (the live inspector's data) and stopping after
`flowMaxSteps`.

`exec` runs one node and returns `(msg, emit)`; a returned `(nil, false)` drops the branch — a
threshold not met, a deadband within tolerance, a `throttle` node inside its window (P4), a script
that returned `null`, or any sink (`debug`/`notify`/`command`/`derived_metric`/`mqtt_out`).

`throttle` (P4) is stateful (`compiledFlow.lastPass`, keyed like `deadband`) but timer-free: it only
ever DROPS a message that arrives inside its window, never defers or replays one after the fact, so
unlike a real rate-limiter it cannot itself become a source of a delayed loop.

## Key Type: flowDeps / flowNotifier / deviceResolver / readingSink

```go
type flowDeps struct {
    issuer       commandIssuer  // the ONE guarded actuation entry point (CommandService)
    flowNotifier flowNotifier
    devices      deviceResolver
    writer       readingSink
    publish      mqttPublish    // broker.Publish seam an mqtt_out node uses (P4)
    topics       topicGuard     // reserves device command topics to the guarded path
    logf         func(string, ...any)
}

type mqttPublish func(topic string, payload []byte, retain bool, qos byte) error

type topicGuard interface {
    ReservedTopic(ctx context.Context, topic string) (deviceKey, command string, reserved bool)
    RecordOffPathRefusal(ctx context.Context, deviceKey, command, topic, actorName string)
}
```

`topicGuard` is satisfied by `*CommandService`, which `app.go` passes to `NewFlowRuntime` **as well
as** passing it as the `commandIssuer` — once as the guarded way in, once as the thing that stops
there being another. A nil guard leaves `mqtt_out` unrestricted, which is right for a unit test with
no devices and wrong for production, so the live bench is what proves the wiring.

The three collaborators (`flowNotifier`, `deviceResolver`, `readingSink`) are interfaces so the
runtime is unit-testable with fakes (`flows_test.go.md`); the production types are
`*notification.Service`, `*DeviceService`, and `*ReadingWriter`.

## Output nodes — the only things that reach outside the sandbox

- **`doNotify`** publishes a `notification.Notification` with `Source: "flow:<name>"` — a first-class
  alert, indistinguishable from a rule's except for its source.
- **`doCommand`** — THE only node type that can actuate. Resolves the target device by natural key
  (`deviceResolver.GetByKey`) and issues through `CommandService.Issue`
  (`services/commands.go.md`), which re-applies every gate (actuation-enabled, admin intent,
  declared bounds, rate limit, audit, never auto-retried). The flow is recorded as the actor
  (`"flow:<name>"`, id 0), so the audit trail shows exactly what commanded the device.
- **`doDerived`** (P3) persists a computed value as a telemetry reading via `readingSink.Enqueue` —
  the "derived metric" of the Layer B solar-workspace design (net grid, self-consumption, battery
  autonomy). It writes straight to the reading store under a target device's namespace, so the
  value is stored, rolled up and charted like any other reading. It deliberately does NOT re-enter
  the ingest pipeline — a derived write that fed back through the flow tap could loop — and a flow
  that wants to alert on its derived value has a threshold->notify branch for exactly that.
- **`doMqttOut`** (P4) publishes the message payload to an MQTT topic via the `publish` seam
  (`broker.Publish` in production). It publishes DATA outward — a processed value fed to another
  system or a home-automation subscriber. `payloadBytes` renders a number as its shortest decimal,
  a string as-is, and anything else as JSON. Publishing is one-way OUT of the hub, so it cannot
  itself loop back into ingest.

  **It may not publish a COMMAND, and that is enforced rather than assumed.** The `publish` seam is
  the server's OWN broker handle, subject to no ACL, so an `mqtt_out` node aimed at a device's
  command topic used to move a real relay whose actuation was switched off, outside the declared
  bounds, past the duty-cycle limit, with nothing written down — every gate in
  `services/commands.go.md` bypassed at once by the one node in the palette whose job is not
  actuation. `doMqttOut` now asks `topicGuard.ReservedTopic` first and, on a reserved topic,
  refuses and records the attempt (`RecordOffPathRefusal`, actor `flow:<name>`) instead of
  publishing. `command` is the only way a flow actuates a device. Found live —
  `tools/fleetbench/bench_iotsan_actuation.py`.

## Key Type: debugRing (inspector)

```go
type debugRing struct { mu sync.Mutex; last map[string]debugEntry; count int64 }
func (d *debugRing) record(nodeId string, m *flowMessage)
func (d *debugRing) snapshot() map[string]debugEntry
```

Keeps the latest message seen at each node plus an execution count, for the live inspector. It is
the one structure a non-worker goroutine (the HTTP debug endpoint) reads, so it carries its own
lock.

## Key Type: FlowRuntime

```go
func NewFlowRuntime(svc *FlowService, issuer commandIssuer, notif flowNotifier, devices deviceResolver, writer readingSink, publish mqttPublish, logf func(string, ...any)) *FlowRuntime
func (r *FlowRuntime) OnReading(ctx context.Context, dev *entities.IotDevice, key string, value float64, nowSec int64)
func (r *FlowRuntime) SignalReload()
func (r *FlowRuntime) Run(ctx context.Context, reconcileEvery time.Duration)
func (r *FlowRuntime) DebugSnapshot(flowId int64) map[string]debugEntry
func (r *FlowRuntime) TestRun(ctx context.Context, flow *entities.IotFlow, seed float64) (map[string]debugEntry, error)
```

Its compiled/index state is touched by the worker goroutine (reconcile + dispatch) and, read-only,
by the debug endpoint — guarded by `mu`. Actual JS execution only ever happens on the worker
goroutine.

`OnReading` is the tap the ingest pipeline calls for every decoded sample (mirrors
`RuleService.OnReading`, `rules.go.md`). It only ENQUEUES — it must never block ingest — and sheds
the newest event (counting it in `dropped`) if the buffer is full.

`Run` is the supervised worker loop (`app.go`'s `safego.Supervise`d `"myiotsan.flows"` task):
reconcile on `reconcileEvery` (const `flowReconcileInterval` = 30s in `app.go`) + on `SignalReload`,
dispatch events in between. `reload` reconciles compiled flows against the enabled rows: an
UNCHANGED graph (`sig` matches) is KEPT — preserving its sandbox's per-flow context state
(`flow.get`/`flow.set`) across reconciles, the mechanism a self-consumption calculation needs to
combine two streams; only an added or edited flow is recompiled; a removed/disabled flow is
dropped. A compile error skips that one flow, logged, never stops the others.

`TestRun` compiles a flow OFF the worker (its own throwaway sandbox, so it never touches the live
runtime's state or another goroutine's goja runtime) and injects a synthetic value at every input
node, returning the resulting per-node snapshot. This backs `POST /flows/{id}/run`
(`apis/flows.go.md`) — an OUTPUT node still acts for real (a notify really publishes, a command
really routes through the guarded path), which is the point of a test-fire.

## Notes

- `indexKey(deviceKey, key)` is the `deviceKey\x00key` composite the runtime indexes bound inputs
  by, so a dispatched reading reaches every flow's input node bound to that (device, key) pair.
- Wired in `apps/myiotsan/app/app.go` alongside the ingest spine: `ingest.SetFlows(flowRuntime)`
  taps the same decoded-sample stream `RuleService.OnReading` does (see `services/ingest.go.md`).
  `app.go` passes `broker.Publish` as the `mqttPublish` dep (P4), so an `mqtt_out` node reaches the
  same embedded broker every device publishes into.
