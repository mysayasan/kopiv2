# Module: infra/iot/modbus/integration_test.go

## Purpose

The live-hardware-shaped proof for the whole Modbus/SunSpec driver: drives `PollOnce` and
`WriteConfirm` against a **running** `tools/sunspec-sim` over real Modbus TCP, rather than a
synthetic in-memory register bank. It is the one test in this package that exercises the actual
"mixed protocol" case the solar system workspace (§8g of `docs/MYIOTSAN_PLAN.md`) must compose.

## Key Function: TestLiveSimulator

Skipped unless `MODBUS_SIM_ADDR` is set, so the rest of the package's unit tests stay hermetic
(`go test ./infra/iot/modbus/...` never needs a running process). Run it with:

```sh
go run ./tools/sunspec-sim &
MODBUS_SIM_ADDR=127.0.0.1:1502 go test ./infra/iot/modbus/ -run Live -v
```

It reads all three of the simulator's personas and confirms a guarded write:

- **unit 1** — the SunSpec hybrid inverter (Common + 103 + 123 + 124 + 203): polls via
  `ModeSunSpec`, checks the nameplate and every expected role-prefixed key
  (`inv_ac_power`/`inv_ac_energy`/`batt_soc`/`grid_ac_power`/`ctl_w_max_lim_pct`) is present.
- **unit 2** — a standalone SunSpec meter (Common + 203 only, a *shorter* chain): proves
  `grid_ac_power` decodes and that a device with no inverter block emits no `inv_ac_power` — the
  chain, not a fixed schema, determines what a device reports.
- **unit 3** — the non-SunSpec vendor inverter: reads it through a `RegisterMap` mirroring the
  vendor register contract documented in `tools/sunspec-sim/devices.go`, proving the manual-map
  path normalises to the same `codec.Sample` shape as the SunSpec path.
- **Guarded control**: enables curtailment on unit 1 (`WMaxLim_Ena=1`) then calls `WriteConfirm` on
  `WMaxLimPct`, proving the read-back a guarded write depends on actually changes on a live device
  (the simulator honours control writes, unlike a purely passive read-only fake).

## Notes

- `index` is a small test helper turning `[]codec.Sample` into a `map[string]float64` for
  assertions.
- This test is the reason `tools/sunspec-sim` exists as a *running process* rather than only unit
  tests of its own — the driver needs something to dial.
