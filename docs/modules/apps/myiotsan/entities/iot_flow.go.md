# Module: apps/myiotsan/entities/iot_flow.go

## Purpose

`IotFlow` is a saved, executable data-flow graph — the myiotsan equivalent of a Node-RED flow.
It is authored on the visual canvas as a set of NODES (inputs that emit on a new reading,
transforms that reshape the message — including arbitrary sandboxed JavaScript — logic that
routes it, and outputs that notify or actuate) joined by WIRES. The runtime
(`services/flow_runtime.go.md`) compiles the graph and runs it against the live telemetry stream.

## Key Type: IotFlow

```go
type IotFlow struct {
    Id          int64
    Slug        string // ukey
    Name        string
    Description string
    Category    string
    Enabled     bool   // soft off switch: disabled flows are not compiled, consume no readings
    Graph       string // the node/wire document as JSON — opaque here, validated by services.parseGraph
    Builtin     bool   // a shipped sample flow: usable and copyable but not deletable
    CreatedBy, CreatedAt, UpdatedBy, UpdatedAt int64/int64
}
```

The whole node/wire graph is stored as ONE JSON document in `Graph` rather than as child rows. A
graph is a picture with positions and per-node config; it is edited and saved wholesale, and it
travels as one portable unit — the same reason a drawn detection zone is stored as a serialized
string in mymatasan. Node device references inside the graph use NATURAL keys (`deviceKey` +
telemetry key), never database ids, so a flow stays portable across installs and a template binds
by name (see `flowSlots`/`Instantiate` in `services/flows.go.md`).

A flow is shipped (`Builtin`) or site-authored, exactly like a `DeviceProfile`: builtins are seeded
from a catalog (`services/flow_catalog.go.md`) and can be used and copied but not deleted, so a
site cannot break a shipped sample by tidying up.

## Notes

- **CRUX — the safety invariant**: nothing in a flow can actuate except a dedicated OUTPUT node,
  and the command output routes through the ONE guarded chokepoint (`CommandService.Issue`), so
  even an arbitrary-JavaScript node can only shape a value that `Issue` then re-validates against
  every gate (actuation-enabled, admin, declared bounds, rate limit, audit, never auto-retried). A
  flow is convenience and computation, not a new authority. This is the design decision that
  reverses `docs/MYIOTSAN_PLAN.md` §8g's original "no visual node-graph editor" scope line — see
  §8i there for the full rationale.
- Registered in `Entities()` (`apps/myiotsan/app/app.go.md`).
