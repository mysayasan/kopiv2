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
flowScriptTimeout   = 100 * time.Millisecond // fences every JS call; interrupted script's node fails, rest of flow unaffected
flowMaxSteps        = 1000                   // bounds one event's propagation (defence in depth; cycles already rejected at save)
flowEventQueue      = 4096                   // ingest -> runtime buffer; overflow DROPS the newest event and counts it
flowEventBudget     = time.Second            // bounds what ONE reading may cost across the WHOLE graph, not just one script
flowQuarantineAfter = 5                      // consecutive script TIMEOUTS (not thrown errors) before a flow is stopped
```

A flow is a convenience layer over telemetry, never the system of record, so shedding load under
backpressure is the deliberate choice — never blocking ingest.

`flowScriptTimeout` fences a single script; nothing fenced the EVENT until `flowEventBudget`. A
graph is allowed `maxFlowNodes` (200) nodes and a propagation `flowMaxSteps` of them, so a graph
of scripts that each individually finish inside their own budget — nothing misbehaving by any rule
the runtime could see — could legally spend a hundred seconds of the one shared worker on ONE
reading. Measured live: a 190-node graph of 80ms scripts held every other flow in the install for
15 seconds per sample. `onInput` now stamps a `flowRun{deadline: time.Now().Add(flowEventBudget)}`
and `visit` checks it before running each node, stopping the event (logged, once per occurrence)
rather than the flow — the graph is simply too slow to run on every sample.

`flowQuarantineAfter` distinguishes a script that THROWS (ordinary — an unexpected payload,
costs nothing, does not count) from one that TIMES OUT (costs the whole per-script budget out of a
worker every other flow is queued behind). Five consecutive timeouts — tracked in
`compiledFlow.timeouts`, reset by any successful run — stop the flow (`compiledFlow.quarantined`)
and publish a notification naming it; editing and re-saving the flow recompiles it and clears the
quarantine.

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

    // Quarantine state. `timeouts` is touched only by the worker goroutine (the only thing that
    // executes a flow); `quarantined` is also READ by the status endpoint on an HTTP goroutine, so
    // it is an atomic.Bool rather than a plain bool.
    timeouts    int
    quarantined atomic.Bool
}

// flowRun travels WITH one event as it propagates: how many nodes it has passed through, and when
// its budget runs out. Both bounds belong to the event, not to a node — what is being protected is
// the worker every other flow in the install is waiting on.
type flowRun struct {
    steps    int
    deadline time.Time
}

func compileFlow(id int64, name, rawGraph string, deps *flowDeps) (*compiledFlow, error)
func (cf *compiledFlow) onInput(ctx context.Context, nodeId string, msg *flowMessage)
func (cf *compiledFlow) exec(ctx context.Context, node *flowNode, in *flowMessage) (*flowMessage, bool)
```

`compileFlow` parses + validates the graph (`parseGraph`), compiles every code-bearing node
(`function`/`expression`/`switch`, via `flows.go.md`'s `nodeScript` — the identical source the
save-time `checkScripts` compiles, so a flow that validated cannot fail here for a reason its
author was never shown) into the sandbox, and indexes inputs and wires. A compile error means the
flow is SKIPPED by the runtime; it is also now recorded (`FlowRuntime.broken`) and reported once to
the notification feed, rather than only logged.

`onInput` refuses a `quarantined` flow outright (no sandbox touched at all) and otherwise seeds a
`flowRun` with a `flowEventBudget` deadline before calling `visit`. `visit` propagates the message
depth-first through the wires, recording each node's received message into `debug` (the live
inspector's data), and stops the event — not the flow — if it exceeds `flowMaxSteps` OR the run's
deadline, whichever comes first.

`exec` runs one node and returns `(msg, emit)`; a returned `(nil, false)` drops the branch — a
threshold not met, a deadband within tolerance, a `throttle` node inside its window (P4), a script
that returned `null`, or any sink (`debug`/`notify`/`command`/`derived_metric`/`mqtt_out`). A
script node additionally routes its error, if any, through `noteScriptError` (below) before
dropping the branch, and resets `timeouts` to 0 on a successful run.

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

## Quarantine: `noteScriptError`

```go
func (cf *compiledFlow) noteScriptError(ctx context.Context, nodeId string, err error)
```

Called from `exec` whenever a script node's `sandbox.run` returns an error. It ignores an ordinary
thrown error outright (`isScriptTimeout`, `flow_eval.go.md`) — a script meeting a payload shape it
did not expect is routine and free. Only a TIMEOUT increments `cf.timeouts`; at
`flowQuarantineAfter` consecutive timeouts it sets `cf.quarantined` and publishes one
`notification.CategorySystem`/Warning notification naming the flow and the node
(`Source: "flow:<name>"`, `Data: {"flowId", "node", "reason": "quarantined"}`). Any run that
returns without timing out resets `cf.timeouts` to 0 first, in `exec`, so an occasionally-slow
script is never quarantined for being merely close to its budget.

## Key Type: FlowRuntime

```go
func NewFlowRuntime(svc *FlowService, issuer commandIssuer, notif flowNotifier, devices deviceResolver, writer readingSink, publish mqttPublish, topics topicGuard, logf func(string, ...any)) *FlowRuntime
func (r *FlowRuntime) OnReading(ctx context.Context, dev *entities.IotDevice, key string, value float64, nowSec int64)
func (r *FlowRuntime) SignalReload()
func (r *FlowRuntime) Run(ctx context.Context, reconcileEvery time.Duration)
func (r *FlowRuntime) DebugSnapshot(flowId int64) map[string]debugEntry
func (r *FlowRuntime) TestRun(ctx context.Context, flow *entities.IotFlow, seed float64) (map[string]debugEntry, error)
func (r *FlowRuntime) States() map[int64]FlowState
func (r *FlowRuntime) Stats() FlowStats
```

Its compiled/index state is touched by the worker goroutine (reconcile + dispatch) and, read-only,
by the debug endpoint and the flows API (`States`/`Stats`) — guarded by `mu`. Actual JS execution
only ever happens on the worker goroutine.

`OnReading` is the tap the ingest pipeline calls for every decoded sample (mirrors
`RuleService.OnReading`, `rules.go.md`). It only ENQUEUES — it must never block ingest — and sheds
the newest event if the buffer is full, incrementing the atomic `dropped` counter (an `int64`
before this, written from whichever goroutine delivered the reading — the broker hook or the
Modbus poller — and read by the stats endpoint; a race until now). It logs on a RAMP (the 1st drop,
then every 1000th) rather than per event, so a sustained overload cannot make the log itself the
bottleneck.

`Run` is the supervised worker loop (`app.go`'s `safego.Supervise`d `"myiotsan.flows"` task):
reconcile on `reconcileEvery` (const `flowReconcileInterval` = 30s in `app.go`) + on `SignalReload`,
dispatch events in between. `reload` reconciles compiled flows against the enabled rows: an
UNCHANGED graph (`sig` matches) is KEPT — preserving its sandbox's per-flow context state
(`flow.get`/`flow.set`) across reconciles, the mechanism a self-consumption calculation needs to
combine two streams; only an added or edited flow is recompiled; a removed/disabled flow is
dropped. A compile error skips that one flow and now, in addition to being logged, is recorded into
`r.broken[id] = err.Error()` and reported once via `reportBroken` — before this the only trace of
an enabled-but-uncompilable flow was one INFO log line, and reaching this path today should mean an
IMPORTED flow or a row from an older build, since `flows.go.md`'s `checkScripts` now refuses the
common case (a typo) at save. `r.told` remembers which failure message has already been reported
per flow id so a reconcile every 30s does not re-notify every cycle; an id that stops being broken
(fixed or removed) is dropped from `told` so a FUTURE failure is reported again.

`TestRun` compiles a flow OFF the worker (its own throwaway sandbox, so it never touches the live
runtime's state or another goroutine's goja runtime) and injects a synthetic value at every input
node, returning the resulting per-node snapshot. This backs `POST /flows/{id}/run`
(`apis/flows.go.md`) — an OUTPUT node still acts for real (a notify really publishes, a command
really routes through the guarded path), which is the point of a test-fire.

## Key Type: FlowState / FlowStats — what the runtime tells the outside world

```go
type FlowState struct {
    State  string `json:"state"`            // "running" | "error" | "quarantined"
    Detail string `json:"detail,omitempty"` // why, when not running
}
func (r *FlowRuntime) States() map[int64]FlowState // by flow id, for every flow the runtime has an opinion about

type FlowStats struct {
    Compiled    int   `json:"compiled"`
    Broken      int   `json:"broken"`
    Quarantined int   `json:"quarantined"`
    Bindings    int   `json:"bindings"`
    Queued      int   `json:"queued"`
    Capacity    int   `json:"capacity"`
    Dropped     int64 `json:"dropped"`
}
func (r *FlowRuntime) Stats() FlowStats
```

`States` is what `apis/flows.go.md`'s `GET /flows` and `GET /flows/{id}` render as `runtimeState`
alongside the stored `enabled` column — the two questions ("was this meant to run" vs "is it
running") used to be indistinguishable on screen. A flow in `r.broken` reports `"error"`; a
compiled-but-`quarantined` flow reports `"quarantined"` with a fixed detail string; anything else
compiled reports `"running"`. A flow the runtime has no opinion about at all (not yet reconciled)
is left for the caller to render as `"starting"`.

`Stats` backs `GET /flows/stats` (`apis/flows.go.md`) and the metrics sampler
(`services/metrics.go.md`'s `flowStatSource`). `Dropped` is the field this whole type exists for:
before it, the runtime counted shed events into a plain field nothing ever read (the comment said
"for logging" and no line logged it), so a flow runtime falling behind under load had no symptom
at all — readings still land, charts still draw, only the automation on top silently stops firing.

## Notes

- `indexKey(deviceKey, key)` is the `deviceKey\x00key` composite the runtime indexes bound inputs
  by, so a dispatched reading reaches every flow's input node bound to that (device, key) pair.
- Wired in `apps/myiotsan/app/app.go` alongside the ingest spine: `ingest.SetFlows(flowRuntime)`
  taps the same decoded-sample stream `RuleService.OnReading` does (see `services/ingest.go.md`).
  `app.go` passes `broker.Publish` as the `mqttPublish` dep (P4), so an `mqtt_out` node reaches the
  same embedded broker every device publishes into. `services.RunMetricsSampler` is now wired
  AFTER the flow runtime is constructed (moved from immediately after the ingest spine) because the
  flow gauges need `flowRuntime` — see `app/app.go.md`.
