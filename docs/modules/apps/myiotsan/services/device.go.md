# Module: apps/myiotsan/services/device.go

## Purpose

The device inventory service, and — because a device's inventory record IS its credential
record — also the embedded broker's `mqtt.Authenticator`. A device not in this table cannot
connect; there is no second credential store to drift out of sync, and deleting a device really
does revoke it.

## Key Type: DeviceService

```go
func NewDeviceService(db dbsql.IDbCrud, logf func(string, ...any)) *DeviceService
```

CRUD plus the authenticator surface, on `dbsql.IGenericRepo[entities.IotDevice]`.

### CRUD

- `List`/`GetById`/`GetByKey` (resolves by `DeviceKey`, the wire identity).
- `Create(ctx, CreateDeviceRequest, actor)` → `*ProvisionedDevice`. Requires a non-empty
  `DeviceKey`; rejects a duplicate with `ErrDeviceKeyTaken`. `Password` empty means one is
  **generated** (`generatePassword`, 24 bytes of crypto/rand, base64) and returned exactly once
  in the response — the same reasoning as the app's own bootstrap admin: a shipped or defaulted
  device password is a fleet-wide backdoor. The plaintext is never stored; only its bcrypt hash
  is.
- `Update(ctx, id, UpdateDeviceRequest, actor)` — no `Password` field by design: rotating a
  credential is its own endpoint (`RotatePassword`), so an ordinary edit cannot silently change
  one.
- `RotatePassword(ctx, id, actor)` — issues and returns a new credential once.
- `Delete(ctx, id)` — removes the device row. **Its readings are left in place deliberately**:
  the history of what a sensor saw is evidence, and must not evaporate because hardware was
  decommissioned. Retention purges them on the normal schedule (`services.TelemetryService`).

### The broker's authenticator

- `AuthenticateDevice(ctx, clientId, password) (int64, bool)` — satisfies `mqtt.Authenticator`.
  Resolves by `DeviceKey`, refuses a disabled device (the `Enabled` toggle has to mean something
  at the wire, or "disabled" is just a table label), then `bcrypt.CompareHashAndPassword`.
  **Distinguishes "could not check" from "wrong password"**: a database-unreachable error is
  logged distinctly and refused (fail closed) rather than reported as a bad credential — telling
  an operator "bad password" when the truth is "I could not look it up" sends them to debug the
  wrong machine.
- `AuthorizeTopic(ctx, deviceId, clientId, topic, write) bool` — satisfies `mqtt.Authenticator`.
  The rule: the device's own key must appear in the topic. Without this, one compromised sensor
  could publish readings on behalf of every other sensor in the building — forging a "no smoke
  detected" for a device that is, in fact, on fire.
- `TouchSeen(ctx, deviceId, nowSec)` — called on EVERY publish (hot path), so the database write
  is **throttled** to at most once per `lastSeenWriteInterval` (30s) per device — the offline
  detector needs minutes of resolution, not milliseconds. Deliberately NOT deadbanded: a device
  faithfully reporting an unchanged value is alive, and gating liveness behind the deadband
  would make a stable sensor look dead.
- `MarkHealth(ctx, deviceId, health)` — used by the offline sweep.

## Notes

- `isNoResultErr` — shared helper (used across this package) that recognizes the generic repo's
  "no result found" sentinel AND its **"total affected: 0"** error, which the repo raises when a
  `Delete` matches zero rows. This second case is what a delete-then-insert seed against an
  empty table produces — the profile seeder's `replaceKeys` does exactly that on first boot, and
  treating "0 rows" as fatal is the bug that **panicked the app on its very first startup**
  before this guard existed.
- `generatePassword` mints real entropy (24 bytes, base64 URL-safe) for device credentials.
