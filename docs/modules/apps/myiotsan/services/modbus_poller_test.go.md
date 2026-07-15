# Module: apps/myiotsan/services/modbus_poller_test.go

## Purpose

Hermetic proof that `modbus_poller.go`'s decode plumbing is correct end to end, without a
database, a live device, or `tools/sunspec-sim` running — a fake register bank is enough to pin
the two shipped Modbus profiles' behavior.

## Responsibilities

- `TestPtypeOf` — pins the `RegKind` string -> `modbus.PType` mapping (case/whitespace-insensitive)
  and its rejection of an unknown kind.
- `TestGenericSunSpecHasNoRegisterBindings` — guards the design: `generic-sunspec-solar` is
  `ModbusMode: "sunspec"`, so its keys must carry NO register bindings (they are discovered, not
  declared) — `registerMapFromKeys` on its keys must error, and a regression that accidentally
  gave the builtin register bindings would be caught here rather than silently building a useless
  map.
- `TestHuaweiBuiltinDecodes` — the end-to-end proof of the shipped `huawei-sun2000` profile: its
  declared `SaveTelemetryKey`s become a `modbus.RegisterMap` via `registerMapFromKeys`, and that
  map decodes a `fakeBank` of raw registers (Huawei's actual scale/word-order conventions) into the
  right physical values — including the **sign** on a grid export and a battery discharge (both
  negative), the classic solar footgun this whole binding design (`telemetry_key.go.md`) exists to
  get right.

## Notes

- `fakeBank` (a `map[int]uint16` satisfying `modbus.Reader`) and `toEntityKeys` (converts
  `SaveTelemetryKey` to `*entities.TelemetryKey`, the shape `registerMapFromKeys` takes) are the
  test-only glue between the profile catalog's DTOs and the driver's `RegisterMap`.
- `findBuiltin` looks a profile up by slug out of `builtinProfiles()` (`profile_catalog.go.md`)
  directly — no `ProfileService`/DB involved — so this test exercises the exact data the seeder
  ships, not a hand-authored fixture that could drift from it.
- Runs in the normal unit suite (`go test ./apps/myiotsan/services/...`); the live-hardware-shaped
  equivalent is `infra/iot/modbus/integration_test.go`'s unit 4, which polls
  `tools/sunspec-sim`'s Huawei persona over real Modbus TCP through the identical register
  addresses.
