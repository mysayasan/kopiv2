# Module: apps/myiotsan/apis/flows.go

## Purpose

Registers the flow engine's endpoints — CRUD, import/export, templates, and the live test-fire/
debug inspector — under `/api/flows`. Thin HTTP layer over `services.FlowService` and
`services.FlowRuntime` (`services/flows.go.md`, `services/flow_runtime.go.md`); no gate lives here.

## Responsibilities

- `NewFlowsApi(router, flows, runtime)` mounts, all under `/flows` (literal paths before `/{id}`
  so they win the mux match):
  - `GET/POST /flows` — list / create (`services.SaveFlowRequest`). Every row in the list, and the
    single row from detail, is flattened through `withRuntimeState` (below) to add `runtimeState`
    and `runtimeDetail` from `FlowRuntime.States()`.
  - `POST /flows/import` — `FlowService.Import`, body capped at 256KB.
  - `GET /flows/stats` — `FlowRuntime.Stats()` (`services.FlowStats`): compiled/broken/quarantined
    flow counts, bound (device,key) pairs, worker queue depth/capacity, and events dropped under
    backpressure. Mounted before `/{id}` for the same routing reason as `/import`.
  - `GET /flows/{id}` — detail; `PUT/DELETE /flows/{id}` — update/delete.
  - `GET /flows/{id}/export` — `FlowExport` document.
  - `GET /flows/{id}/slots` — the device-role slots a flow declares (`{"slots": [...]}`); empty
    means a concrete flow, non-empty means a template.
  - `POST /flows/{id}/instantiate` — binds `{name, bindings}` (slot name -> device key) and stamps
    out a concrete flow.
  - `POST /flows/{id}/run` — `FlowRuntime.TestRun`: injects a synthetic seed value at every input
    node and returns the resulting per-node debug snapshot (`{"nodes": {...}}`). Runs off the live
    runtime (its own throwaway sandbox), but an OUTPUT node still acts for REAL — a notify really
    publishes, a command really routes through the guarded path — which is the point of a
    test-fire. The body is optional; an empty POST seeds `0`.
  - `GET /flows/{id}/debug` — `FlowRuntime.DebugSnapshot`: the latest per-node values of a
    currently-running (enabled and compiled) flow, for the live inspector. Empty if the flow is
    disabled or never enabled.

## RBAC — admin-only in full

Reading and authoring a flow are ordinary CRUD; TEST-RUNNING one exercises the runtime (and, via
an output node, CAN actuate through the guarded command path), so the whole area is admin-only —
the RBAC matrix (`services.Policy()`, `services/rbac.go.md`) denies `/api/flows` to everyone below
admin, and the settings-side `requireAdmin` gate (frontend) is defence in depth. A flow's debug
snapshot is the live inspector's data, also admin-only.

## withRuntimeState — what "enabled" says vs what is actually running

```go
func withRuntimeState(flow *entities.IotFlow, states map[int64]services.FlowState) map[string]any
```

Renders a flow row FLATTENED (marshalled to a map, then two keys added) rather than wrapped, so
every existing list caller keeps working unchanged. `enabled` answers "was this flow meant to run";
until this, nothing rendered the runtime's answer to the different question "is it running", so a
flow whose script would not compile, or one stopped for running away, showed on the canvas exactly
like a healthy one. The mapping: `!flow.Enabled` → `"disabled"`; known in `states` → the state
reported (`"error"`/`"quarantined"`/`"running"`) plus `runtimeDetail` when the state carries one;
enabled but unknown to the runtime (not yet reconciled) → `"starting"`.

## Notes

- Thin layer over `services.FlowService`/`services.FlowRuntime`; no validation or gate logic lives
  here (validation is `services.parseGraph` + `services.checkScripts`, the gate is the RBAC matrix
  + `CommandService.Issue` downstream of a command node).
- Shares `readID`/`decode`/`actorId` helpers with the rest of the `apis` package
  (`apis/devices.go.md`).
