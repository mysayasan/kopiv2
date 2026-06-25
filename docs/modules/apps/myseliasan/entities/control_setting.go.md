# Module: apps/myseliasan/entities/control_setting.go

## Purpose

Defines the `ControlSetting` entity: a persistent key-value row for control-plane settings that survive restarts.

## Responsibilities

- Single-purpose KV table; currently used exclusively to store the fleet key under key `pairing.fleetKey`.
- `Key` has a unique index (`ukey:"key"`) so upsert logic can use `GetByUnique` without a separate lookup query.

## Notes

- The fleet key is stored in plaintext in the myseliasan DB (the control plane is not a field device and does not use `infra/atrest` encryption for this value). Operators should protect the myseliasan database file accordingly.
