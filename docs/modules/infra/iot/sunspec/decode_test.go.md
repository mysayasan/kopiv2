# Module: infra/iot/sunspec/decode_test.go

## Purpose

Pins the two things a SunSpec decoder must get exactly right — scale-factor arithmetic and the
role-prefix collision fix — against synthetic register banks, plus the chain-walking and
no-marker fallback behaviour.

## Responsibilities

- `TestDecodeInverterScaling` — one of every `Kind` (`KI16`, `KU16`, `KAcc32`, `KEnum`) decodes
  through its scale-factor register to the right physical value (e.g. `ac_current` raw `634` with
  `SF=-2` → `6.34 A`).
- `TestDecodeRolePrefix` — a chain with an inverter, a meter, and a battery block does **not**
  collide on `ac_power`: `inv_ac_power`, `grid_ac_power`, and `batt_soc` are all present and
  distinct.
- `TestWalkChain` — builds a minimal one-model chain in a fake in-memory register bank and proves
  `Discover`/`Walk` find the marker, follow the length to the terminator, and decode the model
  inside.
- `TestNoMarker` — a bank with no `SunS` marker returns `ErrNoMarker` (the manual-register-map
  fallback signal), not a crash or a zeroed device.

## Notes

- Pure unit tests against `decode.go`/`model.go` using an in-memory `fakeReader`; no real Modbus
  socket. The live-hardware-shaped equivalent is
  `infra/iot/modbus`'s `TestLiveSimulator` (`integration_test.go.md`), which runs this same
  decoder against `tools/sunspec-sim` over real Modbus TCP.
