# Module: infra/iot/sunspec/decode.go

## Purpose

Walks and decodes a SunSpec model chain: find the `SunS` marker, read `[id][length][data]…`
blocks until the `0xFFFF` terminator, and turn every block this package knows (`model.go`'s
registry) into `codec.Sample`s. This is the one driver that reads any compliant SunSpec
inverter/meter/battery with no per-model register map — the "don't code per model" payoff.

## Key Function: Discover

```go
func Discover(r Reader) (base int, blocks []Block, err error)
```

Tries the three well-known SunSpec base registers in turn — `40000`, `50000`, `0` — and returns
the base that actually carried the marker plus its decoded chain. Returns `ErrNoMarker` if none
of the three did, which is the caller's (`infra/iot/modbus.Poller`) signal to fall back to a
manual `RegisterMap` for a non-compliant device.

## Key Function: Walk

```go
func Walk(r Reader, base int) ([]Block, error)
```

Verifies the `SunS` marker at `base`, then repeatedly reads a `[id, length]` header and the
following `length` data registers, advancing past each model until it reads the `0xFFFF`
terminator. Bounded to 128 iterations so a corrupt/looping chain cannot hang the poller. A block
whose id is not in `model.go`'s registry is still recorded (with its raw `Regs`) but yields no
samples — `Roles` and `DecodeDevice` both skip unknown ids rather than guess at their layout.

## Key Type: Block / Common

`Block` is one discovered model instance (`ID`, `Offset`, `Length`, `Regs` — the raw data
registers, offset 0 = first data register *after* the id+length header). `Common` is the decoded
nameplate from model 1 (`Mfg`, `Model`, `Version`, `Serial`), read by `DecodeCommon` using `str`
(SunSpec's fixed-length, null-padded, 2-chars-per-register string encoding).

## Key Function: DecodeDevice

```go
func DecodeDevice(blocks []Block) []codec.Sample
```

Decodes every **known** model in a chain into role-prefixed samples (`inv_ac_power`,
`grid_ac_power`, `batt_soc`, `ctl_w_max_lim_pct`, ...) via `decodeField`, which applies a field's
`Kind` (see `model.go`) and, if `SFOff >= 0`, multiplies the raw integer by
`10^(int16 scale-factor register)` — the scale-factor arithmetic that is the actual point of
SunSpec's integer encoding. `Roles` reports which non-control roles (`inv`/`grid`/`batt`) a chain
contains, for the future system workspace to suggest how a discovered device slots into a system
(§8g of `docs/MYIOTSAN_PLAN.md`).

## Notes

- Pure decoding, no I/O beyond the `Reader` calls in `Discover`/`Walk`; `infra/iot/modbus.Poller`
  is the caller that dials a device, calls `Discover`/`Walk`, and hands the result to
  `DecodeDevice` (`infra/iot/modbus/poller.go.md`).
- Covered by `decode_test.go` (`decode_test.go.md`): the scale-factor arithmetic, the role-prefix
  collision case, chain walking from a synthetic register bank, and the no-marker fallback.
- Verified against the real thing, not just synthetic banks: `infra/iot/modbus`'s
  `TestLiveSimulator` (`integration_test.go.md`) runs this exact decoder against
  `tools/sunspec-sim`'s live Modbus TCP chain.
