# Module: infra/iot/modbus/regmap_test.go

## Purpose

Pins the two properties that make `RegisterMap` correct and efficient: mixed-type/scale decoding,
and the single-round-trip span computation.

## Responsibilities

- `TestRegisterMapDecode` — a 32-bit scaled power, a signed 16-bit battery power, and a plain SoC
  each decode through their own `PType`/`Scale` to the right physical value from one fake register
  bank.
- `TestRegisterMapSingleRead` — the computed span (`span()`) covers the lowest to the highest
  register across every point in the map, proving the whole device is read in one round trip
  rather than one read per point.

## Notes

- Pure unit tests against `regmap.go` using an in-memory `fakeReader`; no real Modbus socket. The
  live-hardware-shaped equivalent is `integration_test.go`'s `TestLiveSimulator`, which reads
  `tools/sunspec-sim`'s non-SunSpec vendor inverter through a real `RegisterMap` over real Modbus
  TCP.
