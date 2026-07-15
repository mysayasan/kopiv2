# Module: infra/iot/modbus/regmap_test.go

## Purpose

Pins the three properties that make `RegisterMap` correct and efficient: mixed-type/scale
decoding, the single-round-trip span computation for a packed map, and clustered reads for a
scattered one.

## Responsibilities

- `TestRegisterMapDecode` — a 32-bit scaled power, a signed 16-bit battery power, and a plain SoC
  each decode through their own `PType`/`Scale` to the right physical value from one fake register
  bank.
- `TestRegisterMapSingleRead` — the computed span (`span()`) covers the lowest to the highest
  register across every point in the map, proving a packed device is read in one round trip
  rather than one read per point.
- `TestRegisterMapClusters` — a map whose points are scattered wider than the 125-register Modbus
  read limit (an inverter block near 32000, a battery block near 37000 — the Huawei SUN2000 shape)
  is read as **two** bounded requests, one per block, via `countingReader`, and every point across
  both clusters still decodes to the right scaled/signed value (including a negative `i32` battery
  discharge).

## Notes

- Pure unit tests against `regmap.go` using in-memory readers; no real Modbus socket.
  `countingReader` additionally rejects any read wider than `maxReadRegisters`, so a regression
  that reintroduced a single-span read across a scattered map would fail loudly rather than just
  costing an extra round trip.
- The live-hardware-shaped equivalent is `integration_test.go`'s `TestLiveSimulator`, which reads
  `tools/sunspec-sim`'s non-SunSpec vendor inverter (unit 3) and Huawei SUN2000 persona (unit 4)
  through a real `RegisterMap` over real Modbus TCP, the latter proving the same clustering
  against an actual (simulated) scattered device rather than a synthetic bank.
