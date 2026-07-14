# Module: apps/myiotsan/entities/iot_device.go

## Purpose

Defines the persisted device inventory record — myiotsan's `camera`. One row per physical
sensor or actuator.

## Fields

- Identity: `Id`, `Name`, `DeviceKey` (unique — the device's MQTT client id and the variable
  segment of its topic; what makes the inventory record ALSO the credential record — a device
  not in this table cannot connect to the broker, see `infra/iot/mqtt`).
- Wiring: `Protocol` (`"mqtt"` or `"http"`), `ProfileId` (points at the `DeviceProfile` that
  declares this device's telemetry keys — without one, a device can connect but nothing it
  publishes can be decoded, indexed via `idx:"profile"`).
- Credential: `PasswordHash` (bcrypt, `json:"-"`, never returned).
- Grouping/metadata: `Tag` (indexed; groups devices — "floor-2", "cold-store" — so a rule can
  scope to a set rather than naming each device, the replacement for mymatasan's per-camera
  zone polygon), `Location`, `Vendor`, `Model`, `Serial`, `Firmware`, `Enabled`.
- `ActuationEnabled` — the per-device capability toggle. Devices are read-only by default; a
  command is refused unless this is explicitly on, on top of the admin-only RBAC rule. A bad
  relay write is physically dangerous in a way a bad PTZ move is not, so it takes two
  deliberate acts to enable one. (Command path itself lands in P4.)
- Liveness: `LastSeenAt` (unix seconds, updated on every publish but NOT through the deadbanded
  reading path — a device faithfully reporting an unchanged value must still look alive to the
  offline detector), `Health` (`"online"`/`"offline"`/`"unknown"`).
- Audit fields: created/updated user and timestamps.

## Notes

- Bootstrap creates this table from the registered entity when SQLite or another supported DB
  engine starts.
- `services.DeviceService` (`apps/myiotsan/services/device.go.md`) owns CRUD, credential
  provisioning/rotation, and is also the broker's `mqtt.Authenticator`.
