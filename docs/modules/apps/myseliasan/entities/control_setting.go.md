# Module: apps/myseliasan/entities/control_setting.go

## Purpose

Defines the `ControlSetting` entity: a persistent key-value row for control-plane settings that survive restarts.

## Responsibilities

- Single-purpose KV table, holding a handful of unrelated control-plane rows keyed by a
  dotted-style string: the fleet PSK (`pairing.fleetKey`), the fleet CA / parent leaf key
  material (`pairing.caKey`/`pairing.caCert`/`pairing.parentKey`/`pairing.parentCert`,
  `services/fleet_ca.go.md`), and the in-app settings editor's first-run defaults snapshot
  (`settings.defaults`, `services/settings.go.md`).
- `Key` has a unique index (`ukey:"key"`) so upsert logic can use `GetByUnique` without a separate lookup query.

## Notes

- Not every row is encrypted the same way. The fleet key (`pairing.fleetKey`) has historically been stored in plaintext (the control plane is not a field device and did not originally use `infra/atrest` encryption for this value) — see `services/secret_store.go.md` for which keys route through `encodeSecret`/`decodeSecret` (the CA/parent private keys, the PSK, and now the `settings.defaults` snapshot) when a fleet cipher is configured. Operators should protect the myseliasan database file regardless.
