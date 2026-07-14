# Module: apps/myiotsan/services/enrollment.go

## Purpose

How a device that does not exist yet gets into the inventory.

**The tension it resolves.** The broker's security model is that a device not in `iot_device`
cannot connect — that is what makes the device table the credential store, and what makes
deleting a device actually revoke it. But a building with two hundred door contacts cannot be
onboarded by typing two hundred device keys by hand, and a device that does not exist yet cannot
announce itself.

The hole is opened deliberately, and built to be safe to open, with four load-bearing
properties:

- **TIME-BOXED.** An admin opens a window; it expires on its own (TTL clamped to 1 hour,
  default 10 minutes if unset or out of range). There is no way to leave it open by forgetting
  about it, which is how every "temporary" provisioning mode ends up permanent.
- **SECRET-GATED.** The window mints a one-time key with real entropy (24 bytes of
  `crypto/rand`, base64), shown exactly once and bcrypt-hashed at rest — never readable back.
  Re-opening replaces it, so two keys can never be valid at once.
- **QUARANTINED — the property that actually matters.** An enrolling client's payloads are
  recorded as a `DiscoveredDevice` candidate and nowhere else: no telemetry is stored, no rule is
  evaluated. Somebody who gets into the window can leave junk candidates for an admin to
  decline; they cannot forge a reading, move a chart, or trigger or suppress an alert.
- **CAPPED + AUDITED.** `maxCandidates` (500) stops a flood from filling the disk. Opening a
  window publishes a security event to the notification feed (`app.go`'s wiring), so it cannot
  be done quietly.

Adoption is the deliberate act: the admin sees what the thing actually sends, is told what it
probably is, and mints it a real credential.

## Key Type: Enrollment

```go
func NewEnrollment(db dbsql.IDbCrud, profiles *ProfileService, logf func(string, ...any)) *Enrollment
```

### The window

- `Open(ctx, ttl, actor) (WindowStatus, error)` — mints and bcrypt-hashes a new key, replacing
  any previous window (there is only ever one live key). Returns the plaintext key in
  `WindowStatus.Key`, which is the only time it is ever readable.
- `Close()` — ends the window immediately.
- `Status() WindowStatus` — never carries the key.
- `VerifyKey(password) bool` — checks an enrolling client's credential against the current
  window. A closed or expired window verifies nothing; expiry is enforced here, on every
  connect, not by a sweeper that might not have run yet.

### The quarantine

- `Observe(ctx, deviceKey, topic, payload)` — the ONLY thing a quarantined client's payload is
  allowed to do. Upserts a `DiscoveredDevice` by `DeviceKey`, bumping `MessageCount`/
  `LastSeenAt`/`Topic`/`Payload` (truncated to `maxPayloadSample`, 2000 bytes) and re-running the
  profile suggestion on each observation. New candidates are refused once `maxCandidates` (500)
  is reached.

### The profile suggester

- `suggestProfile(ctx, topic, observed) int64` — guesses what a candidate IS from what it says.
  Score = the fraction of the **profile's** keys the device actually sent (matched against each
  key's `JsonPath` first segment, or the key name itself), plus a `+0.15` bonus if the topic has
  the profile's `TopicTemplate` prefix. Deliberately asymmetric: scoring against the profile's
  keys (not the device's) means a profile that merely shares "battery" with everything does not
  win just because "battery" matched.
- **Below a 0.6 floor it suggests nothing at all.** A wrong suggestion an installer accepts
  without thinking silently mis-decodes every reading that device will ever send — worse than no
  suggestion. Verified live: `{battery, humidity, linkquality, temperature}` correctly suggested
  "Temperature / humidity sensor".

### Adoption

- `Candidates(ctx) ([]*entities.DiscoveredDevice, error)` — chattiest first
  (`MessageCount DESC`) — a device that has spoken forty times is real; one that spoke once may
  have been a passing scan.
- `Adopt(ctx, id, AdoptRequest, devices *DeviceService, actor) (*ProvisionedDevice, error)` —
  promotes a candidate to a real `IotDevice` via `DeviceService.Create` (provisioned read-only:
  `ActuationEnabled: false` — a device does not arrive able to switch a relay just because
  somebody plugged it in), defaulting `ProfileId` to the candidate's suggestion if the admin did
  not override it, then deletes the candidate row. **The enrollment key does not carry over**:
  the device gets its own real, generated credential (returned once, same as any other
  `DeviceService.Create`), so a leaked window key cannot be used to impersonate a device that was
  adopted with it.
- `Reject(ctx, id) error` — discards a candidate.

## Notes

- `SetEnrollment` on both `DeviceService` and `Ingest` wires this in after construction
  (`app.go`), because `Enrollment` itself depends on `ProfileService`, which depends on the db —
  a small ordering knot, not a design smell.
- `payloadKeys`/`topicPrefix`/`truncate` are small package-private helpers exercised directly by
  `enrollment_test.go`.
