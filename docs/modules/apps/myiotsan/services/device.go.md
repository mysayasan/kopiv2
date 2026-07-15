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
- **(P5)** `CreateDeviceRequest`/`UpdateDeviceRequest` carry `Endpoint`/`Unit`, plumbed straight
  onto `entities.IotDevice` — address a POLLED (Modbus) device (`"host:port"` + unit/slave id);
  empty/zero for an MQTT device, which is reached by `DeviceKey` over the broker instead. Read by
  `services.ModbusPoller` (`modbus_poller.go.md`) to build each device's poll target.
- `RotatePassword(ctx, id, actor)` — issues and returns a new credential once.
- `Delete(ctx, id)` — removes the device row. **Its readings are left in place deliberately**:
  the history of what a sensor saw is evidence, and must not evaporate because hardware was
  decommissioned. Retention purges them on the normal schedule (`services.TelemetryService`).

### The broker's authenticator

- `SetEnrollment(e *Enrollment)` — wires the enrollment window in (`app.go`, after construction:
  `Enrollment` itself needs `ProfileService`, which needs the db). `nil` until set, which is
  fine: no window means `AuthenticateDevice` never has anywhere to fall through to.
- `AuthenticateDevice(ctx, clientId, password) (iotmqtt.Principal, bool)` — satisfies
  `mqtt.Authenticator`. **Three outcomes, in this order:**
  1. A known, enabled device with the right password → admitted as itself
     (`Principal{DeviceId: dev.Id}`).
  2. An **unknown** client whose password verifies against an open enrollment window's key →
     admitted **quarantined** (`Principal{Enrolling: true}`) — see
     `services/enrollment.go.md`.
  3. Anything else → refused.

  Resolves by `DeviceKey` first; refuses a **disabled** device outright — the `Enabled` toggle
  has to mean something at the wire, or "disabled" is just a table label, and critically **a
  disabled device does NOT fall through to enrollment**: an admin who switched a device off must
  not have it walk back in through the side door. **Distinguishes "could not check" from "wrong
  password"**: a database-unreachable error is logged distinctly and refused (fail closed)
  rather than reported as a bad credential — telling an operator "bad password" when the truth is
  "I could not look it up" sends them to debug the wrong machine.
- `AuthorizeTopic(ctx, p iotmqtt.Principal, clientId, topic, write) bool` — satisfies
  `mqtt.Authenticator`. The rule: the client's own key must appear in the topic. Without this,
  one compromised sensor could publish readings on behalf of every other sensor in the building —
  forging a "no smoke detected" for a device that is, in fact, on fire. Applies to an
  **enrolling** client exactly as it does to an adopted one, so a device announcing itself cannot
  pollute another device's stream even as a candidate.
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
