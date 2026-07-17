# Module: apps/myiotsan/entities/telemetry_key.go

## Purpose

Declares one datapoint a profile's devices report: its name, its unit, how to pull it out of
the payload, and — the important one — its `Deadband`.

**The deadband IS the storage design.** 100 devices x 10 keys at 1 Hz is 1,000 rows a second,
which SQLite will not absorb and which no amount of batching makes free. A building sensor is
near-static almost all of the time: a room is 21.4 degrees for an hour, a door is closed all
night. Persisting a reading only when it moves by more than the deadband (or when the
heartbeat elapses, so a flat line is still provably alive) collapses that by one to two orders
of magnitude and loses nothing an operator would ask about later. This measured out in
production: 20 devices publishing ~30,000 samples in under a second produced 540 written rows,
98.2% suppressed, zero dropped — see `apps/myiotsan/services/deadband.go.md`.

## Fields

- `Id`, `ProfileId` (indexed, required), `Key`, `Label`, `Unit`.
- `DataType` — `"number"`, `"bool"` or `"string"`. Decides which column (`Num`/`Str` on
  `DeviceReading`) a reading lands in and which rule conditions may be applied.
- `JsonPath` — the dotted path into the payload (`"battery.level"`); empty means the key name
  is itself a top-level field. This is the binding for a PUSH device (MQTT/HTTP).
- **Modbus binding (P5, POLL devices only)** — `Register` (starting register address; `0` with an
  empty `RegKind` means the key is not Modbus-bound, so a mixed profile is harmless), `RegKind`
  (`"u16"`/`"i16"`/`"u32"`/`"i32"`/`"f32"` — the last an IEEE-754 float32, the encoding cheap meters
  like the Eastron SDM630 use), `ScaleFactor` (multiplies the raw integer; `0` is treated as `1`, so
  an unset scale is an identity, not a value annihilated to zero), `WordSwap` (true when a 32-bit
  value is little-word-first — vendors disagree on this, and getting it wrong turns a plausible
  number into a wild one), `RegInput` (reads the point from Modbus INPUT registers, fn 4, instead of
  holding registers, fn 3 — Huawei's map is all-holding, but Sungrow SH and the Eastron meter keep
  their measurements in the input bank; `false`, the default, preserves the original fn-3-only
  path). For a `"sunspec"`-mode profile these are ignored (the device is self-describing, so its
  keys are discovered, not declared); for a `"regmap"`-mode profile they are the whole point. **The
  sign and scale live here deliberately** — import- vs export-positive on a meter, charge- vs
  discharge-positive on a battery, and a 0.1-unit device vs a 1-unit one are all the same class of
  footgun, and they belong to the binding, not to any decode code. Built into a `modbus.RegisterMap`
  by `apps/myiotsan/services.registerMapFromKeys` (`modbus_poller.go.md`).
- `Deadband` — the smallest absolute change worth persisting. `0` means store every sample —
  correct for a door contact, where every transition matters, and wrong for a temperature
  probe, where every flicker of sensor noise would become a row.
- `HeartbeatSeconds` — forces a row even when the value has not moved, so a flat line is
  distinguishable from a dead device and a chart has a point to draw. `0` disables it.
- `Min`/`Max` — the plausible range. A reading outside it is **stored but flagged** (see
  `DeviceReading.Suspect`) — a sensor reporting -3000 degrees is broken, not cold, and dropping
  it would hide that.
- Audit fields: created/updated user and timestamps.

## Notes

- Read on the hot ingest path once per profile (cached by `services.Ingest`, invalidated on
  profile edit) rather than per message.
- Bootstrap creates this table from the registered entity when SQLite or another supported DB
  engine starts.
