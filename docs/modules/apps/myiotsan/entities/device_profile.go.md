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
  one value).
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
