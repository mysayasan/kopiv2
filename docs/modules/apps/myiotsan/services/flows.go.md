# Module: apps/myiotsan/services/flows.go

## Purpose

Owns the flow GRAPH MODEL, its validation, and the CRUD service (`FlowService`). The runtime that
compiles and executes a graph is `flow_runtime.go.md`; the JavaScript substrate is
`flow_eval.go.md`; the sample builtin is `flow_catalog.go.md`; import/export is
`flow_transfer.go.md`.

## The graph document

A flow is authored on the canvas and stored as one JSON document (`entities.IotFlow.Graph`):

```go
type flowGraph struct {
    Nodes []flowNode
    Wires []flowWire
}
type flowNode struct {
    Id     string
    Type   string
    X, Y   float64
    Config map[string]any
}
type flowWire struct {
    From, To flowPort // flowPort{Node string, Port int}
}
```

Node device references use natural keys (`deviceKey` + telemetry key), never database ids, so a
saved flow is portable across installs.

## The P1 node palette

```go
nodeDeviceTelemetry = "device_telemetry" // input: emits when a reading for (deviceKey,key) arrives
nodeFunction        = "function"         // transform: arbitrary sandboxed JS body ending in `return msg`
nodeExpression      = "expression"       // transform: a JS expression -> msg.payload
nodeScale           = "scale"            // transform: payload*factor + offset
nodeThreshold       = "threshold"        // logic: pass only if payload <op> value
nodeSwitch          = "switch"           // logic: pass only if a JS predicate is truthy
nodeDeadband        = "deadband"         // logic: pass only if payload moved >= delta since last pass
nodeThrottle        = "throttle"         // logic: pass at most once per N seconds (rate limit), P4
nodeDebug           = "debug"            // output: record the message for the inspector (a sink)
nodeNotify          = "notify"           // output: publish a notification (a sink)
nodeCommand         = "command"          // output: actuate a device via the GUARDED path (a sink)
nodeDerivedMetric   = "derived_metric"   // output: persist a computed value as a telemetry series (a sink), P3
nodeMqttOut         = "mqtt_out"         // output: publish the payload to an MQTT topic (a sink), P4
```

`codeBearing(t)` reports whether a node type runs a script in the goja sandbox
(`function`/`expression`/`switch`); `knownNodeType(t)` is the closed set every graph is checked
against.

## Key Function: parseGraph

```go
func parseGraph(raw string) (*flowGraph, error)
```

Unmarshals and VALIDATES a graph: known node types only (an unknown type is refused, never
silently ignored — the same closed-default the command layer uses), unique node ids, wires that
reference real nodes, a node ceiling (`maxFlowNodes` = 200, a sanity ceiling not a product limit),
and NO CYCLES (`findCycle`, DFS three-colour — P1 executes a DAG; a cycle would loop a message
forever). An empty string is legal (a freshly created, not-yet-drawn flow). Validation runs at
save (`Create`/`Update`) and again at compile (`flow_runtime.go.md`'s `compileFlow`), so a bad
graph can neither be stored nor run.

## Key Type: FlowService (CRUD)

```go
func NewFlowService(db dbsql.IDbCrud, logf func(string, ...any)) *FlowService
func (s *FlowService) SetOnChange(fn func())
func (s *FlowService) List(ctx context.Context) ([]*entities.IotFlow, error)
func (s *FlowService) ListEnabled(ctx context.Context) ([]*entities.IotFlow, error)
func (s *FlowService) Detail(ctx context.Context, id int64) (*entities.IotFlow, error)
func (s *FlowService) Create(ctx context.Context, req SaveFlowRequest, actor int64) (*entities.IotFlow, error)
func (s *FlowService) Update(ctx context.Context, id int64, req SaveFlowRequest, actor int64) (*entities.IotFlow, error)
func (s *FlowService) Delete(ctx context.Context, id int64) error
```

`SetOnChange` registers the runtime's reload signal (`FlowRuntime.SignalReload`, wired once in
`app.go`); `Create`/`Update`/`Delete` all call `changed()` afterward so enabling, editing or
deleting a flow takes effect with no process restart. `Delete` refuses a builtin flow ("copy it
instead"). `uniqueSlug` derives `slug`, `slug-2`, `slug-3`, … when a name collides.

## Key Type: templates — device-role SLOTS

A flow becomes a reusable TEMPLATE simply by naming a device by a SLOT — a placeholder like
`"$inverter"` in a node's `deviceKey` — instead of a concrete key. Instantiating the template binds
each slot to a real device and stamps out a normal, concrete flow. No separate entity, no schema
change: a template is a flow whose graph still has slots, and an instance is one whose slots are
filled — the "declare once, deploy many" pattern the solar workspace calls for, at the flow level.

```go
func slotName(deviceKey string) (string, bool)          // "$inverter" -> ("inverter", true)
func flowSlots(g *flowGraph) []string                    // unique slots, first-seen order, never nil
func (s *FlowService) Slots(ctx context.Context, id int64) ([]string, error)
func (s *FlowService) Instantiate(ctx context.Context, id int64, name string, bindings map[string]string, actor int64) (*entities.IotFlow, error)
```

`Instantiate` requires every declared slot to be bound (an unbound slot is refused, listing what is
missing), substitutes the bound `deviceKey`s into a copy of the graph, and creates the result
DISABLED — an admin reviews, then enables — and never itself a template (all slots are resolved).
A flow with no slots cannot be instantiated ("copy it instead").

## Notes

- `SaveFlowRequest{Name, Slug, Description, Category, Enabled, Graph}` is the create/update body;
  `Slug` is optional on create (derived from `Name` via `slugify`).
- `cfgString`/`cfgFloat`/`cfgBool` are the free-form `map[string]any` config accessors every node
  type reads its own config through. `cfgFloat` (P4) also parses a string-encoded value — a
  `<select>` field (e.g. the `mqtt_out` QoS picker) stores its option value as text — falling back
  to `strconv.ParseFloat` when the raw value isn't already numeric; payload coercion (`coerceFloat`)
  deliberately stays strict, only config reading is this lenient.
- See `flows_test.go.md` for the pure unit coverage (propagation, sandbox containment, slots).
