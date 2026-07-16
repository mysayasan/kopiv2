# Module: apps/myiotsan/services/flow_catalog.go

## Purpose

Declares the SHIPPED sample flows, seeded at boot the same way device profiles are
(`profile_catalog.go.md`): present-by-slug is skipped so a site never has a tuned flow overwritten,
and a builtin can be used and copied but not deleted.

A builtin flow is a TEMPLATE to learn from and adapt, not a turnkey install: its input nodes name
devices by a SLOT (`"$inverter"`, see `flows.go.md`'s templates section), because the real device
keys are chosen at adoption. It is created `Enabled: false` for the same reason an imported flow is
— it should not try to act until an admin has rebound it to real devices and switched it on.

## Key Function: solarSystemFlow

```go
func solarSystemFlow() builtinFlow
```

The reusable "Solar system" sample the design calls for. It shows the one thing the data-flow model
exists to do — COMBINE two telemetry streams into a derived value — alongside a simple threshold
alert:

```
grid_ac_power ──┐
                ├─▶ [function: self_consumption] ─▶ [debug]
inv_ac_power ───┘
grid_ac_power ──▶ [threshold > 3000 W] ─▶ [notify: high grid import]
```

The `function` node stashes each stream in the per-flow context (`flow.set`/`flow.get`) and
recomputes on every reading — the idiomatic way to join streams when each arrives as its own
message. `$inverter` (a SLOT, not a concrete device) makes this a reusable template; instantiating
it (`FlowService.Instantiate`) binds the slot to a real adopted inverter and stamps out a concrete
flow.

## Key Function: EnsureBuiltins

```go
func (s *FlowService) EnsureBuiltins(ctx context.Context) error
```

Seeds `builtinFlows()` (currently just `solarSystemFlow()`). Present-by-slug is skipped, so a
site's edits to a copied flow are never clobbered. Called from `app.go` at boot, before the flow
runtime starts.

## Notes

- `mustGraph(g flowGraph) string` marshals a graph built from the typed structs, so a builtin is
  guaranteed to be valid JSON that `parseGraph` accepts — a failure here is a programming error
  caught at first boot, not a runtime surprise. Pinned by `flows_test.go.md`'s
  `TestFlow_BuiltinSolarIsATemplate`.
