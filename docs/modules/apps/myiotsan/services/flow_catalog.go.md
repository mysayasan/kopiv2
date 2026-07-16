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

## Key Functions: five more solar templates (all on `$inverter`)

Added alongside the three new register-map Modbus profiles (`profile_catalog.go.md`):

- **`selfConsumptionFlow` (`solar-self-consumption`)** — the same `grid_ac_power`/`inv_ac_power`
  two-stream join as `solarSystemFlow`, but persists the result as a `self_consumption`
  `derived_metric` series instead of only debugging it, so it rides the same rollups/charts as any
  real reading.
- **`selfSufficiencyFlow` (`solar-self-sufficiency`)** — derives self-sufficiency % (solar's share
  of total energy) from the lifetime counters `inv_ac_energy` (PV yield) and
  `grid_energy_imported`, persisted as `self_sufficiency`.
- **`batteryGuardFlow` (`solar-battery-guard`)** — alerts when `batt_soc` drops below 15%, and
  **force-charges** below 8% via a `command` output node (`batt_force = 170`, Sungrow's "Charge"
  value) — the command routes through `CommandService.Issue` like every other actuation path, so it
  inherits every gate and stays inert until the device's actuation is enabled.
- **`exportLimitFlow` (`solar-export-limit`)** — THE control showcase: when export (derived from
  `grid_ac_power`) exceeds ~4.5kW, a `command` node writes the inverter's `export_limit` to 4kW.
  Inert the same way, until actuation is enabled and the register is bench-verified for the model.
- **`inverterHealthFlow` (`solar-inverter-health`)** — alerts on the two silent string-inverter
  failure modes: `inv_temperature` above 60°C, and `inv_operating_state` in an abnormal range
  (`>= 5`, tunable per vendor's state table via the KB).

Every one of these is `mustGraph`-built (see below) and covered by `TestBuiltinFlowsParse`
(`modbus_poller_test.go.md`), which now also asserts at least 6 builtin flows exist.

## Key Function: EnsureBuiltins

```go
func (s *FlowService) EnsureBuiltins(ctx context.Context) error
```

Seeds `builtinFlows()` — the original `solarSystemFlow()` plus the five above (six total).
Present-by-slug is skipped, so a site's edits to a copied flow are never clobbered. Called from
`app.go` at boot, before the flow runtime starts.

## Notes

- `mustGraph(g flowGraph) string` marshals a graph built from the typed structs, so a builtin is
  guaranteed to be valid JSON that `parseGraph` accepts — a failure here is a programming error
  caught at first boot, not a runtime surprise. Pinned by `flows_test.go.md`'s
  `TestFlow_BuiltinSolarIsATemplate`.
