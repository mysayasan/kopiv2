# Module: apps/myiotsan/entities/device_profile.go

## Purpose

Defines a template for a device TYPE: what it publishes, where, and in what shape. This is the
abstraction mymatasan does not have, and it is what makes onboarding scale past a demo —
without it, every one of a hundred identical door sensors would be configured by hand.

## Fields

- Identity: `Id`, `Slug` (unique), `Name`.
- Descriptive: `Vendor`, `Description` — for the human choosing a profile.
- Wire shape: `TopicTemplate` (the MQTT topic a device of this type publishes to, with
  `{deviceKey}` substituted — the de-facto conventions Zigbee2MQTT/Tasmota/Shelly all fit),
  `PayloadFormat` (`"json"`, the overwhelmingly common case, or `"raw"` — the entire payload is
  one value). Both apply to a PUSH transport only.
- **`Transport` (P5)** — `""`/`"mqtt"` (default; every profile shipped before P5) means the device
  PUSHES to the broker; `"modbus"` means the app POLLS it over Modbus TCP
  (`apps/myiotsan/services.ModbusPoller`, `modbus_poller.go.md`). This is the field that turns a
  profile from a topic descriptor into a driver descriptor with no second entity.
- **`ModbusMode`/`ModbusBase`/`PollSeconds` (P5)**, meaningful when `Transport == "modbus"`:
  `ModbusMode` is `"sunspec"` (self-describing — the driver walks the SunSpec model chain and
  DISCOVERS the keys; declared `TelemetryKey`s only supply deadband/heartbeat/range) or `"regmap"`
  (non-SunSpec — every key carries an explicit register binding, see `telemetry_key.go.md`).
  `ModbusBase` is the SunSpec base register (`0` = auto-discover: 40000/50000/0), ignored for
  `"regmap"`. `PollSeconds` is the poll cadence (`0` defaults to 5s) — it governs bus load the way
  the deadband governs storage: a wider interval is fewer round trips, not fewer stored rows.
- `Builtin` — marks a shipped profile. Builtins can be used and copied but not deleted
  (`services.ProfileService.Delete` returns `ErrProfileBuiltin`), so a site cannot break its own
  onboarding by tidying up.
- Audit fields: created/updated user and timestamps.

## Notes

- A profile's datapoints live in the sibling `TelemetryKey` table, one row per key,
  `ProfileId`-scoped.
- Bootstrap creates this table from the registered entity when SQLite or another supported DB
  engine starts.
- `services.ProfileService.EnsureBuiltins` seeds the shipped catalog
  (`apps/myiotsan/services/profile_catalog.go.md`) on every boot, leaving existing profiles
  alone.
