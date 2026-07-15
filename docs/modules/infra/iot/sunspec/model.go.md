# Module: infra/iot/sunspec/model.go

## Purpose

The static SunSpec model registry: which standard model ids this package understands, and where
each model's fields (and their scale-factor registers) sit inside a model's data block. This is
the "don't code per model" table — a device's chain is decoded by looking its model ids up here,
not by writing a per-vendor parser.

## Key Type: Reader

```go
type Reader interface {
    ReadHolding(addr, qty int) ([]uint16, error)
}
```

The minimal Modbus surface the decoder needs. `infra/iot/modbus.Client` satisfies it in
production; tests supply an in-memory fake (`fakeReader` in `decode_test.go`).

## Key Type: Kind

How a field's registers are interpreted: `KU16` (unsigned 16-bit), `KI16` (signed 16-bit),
`KAcc32` (unsigned 32-bit accumulator, hi word first — used for cumulative energy counters),
`KEnum` (unsigned 16-bit status/enum, **never** scaled — an operating-state code is not a
quantity).

## Key Type: Field / ModelDef

`Field` is one decoded datapoint within a model: the telemetry key it becomes (`Key`), the
register offset of its value (`Off`) and, if the field is scaled, the offset of its scale-factor
register (`SFOff`, `-1` for none). `ModelDef` groups a model id's fields under a `Role`
(`"inv"`/`"grid"`/`"batt"`/`"ctl"`) that prefixes every emitted key — the mechanism that keeps a
hybrid inverter's own inverter block and its built-in grid meter from colliding on `ac_power`
(`decode.go`'s `DecodeDevice`).

## The registry

Four field tables, each pinned to the real SunSpec register layout (offsets verified against the
spec and cross-checked by `tools/sunspec-sim/models_test.go`, which pins the same layout from the
producing side):

- `inverterFields` — models 101/102/103 (single/split/three-phase inverter, integer + scale
  factor): AC power/current/voltage, frequency, cumulative AC energy, DC power/voltage,
  temperature, operating state.
- `meterFields` — models 201-204 (single/split/wye/delta meter, integer + scale factor): AC
  power (**positive = import, negative = export** — the sign convention a driver must be pinned
  to, not guessed), current, voltage, frequency, cumulative imported/exported energy.
- `storageFields` — model 124 (battery control): state of charge, charge status, the writable
  `storage_ctl_mode` (decoded so a guarded write to it can be read back), battery voltage, max
  charge power.
- `controlFields` — model 123 (immediate controls): the writable curtailment block,
  `w_max_lim_pct` + `w_max_lim_ena`, decoded for the identical read-back reason.

`registry` (built once at package init) maps every supported model id to its `ModelDef`. `Known`
reports whether an id is one this package decodes at all — an unknown model in a chain is skipped
by the decoder, never guessed at.

## Notes

- Only the **integer + scale-factor** representation of each model is implemented (the float
  variants some vendors use are not); this matches what `tools/sunspec-sim` produces and what real
  hybrid inverters overwhelmingly ship.
- Adding a new standard model (e.g. a DER/reactive-power model) is adding one `Field` table plus a
  `registry` entry — no change to `decode.go`'s walk/decode logic.
