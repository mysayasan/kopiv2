# Module: apps/mymatasan/services/pairing.go

## Purpose

Implements `IPairingService`, the node-side single-parent lock state machine. It persists all pairing state in the existing `RuntimeSetting` KV store (no new DB table) and enforces first-adopter-wins semantics under a mutex so two simultaneous adopt calls cannot both win.

## Responsibilities

- Persist `pairing.state` (JSON blob: paired flag, parentId, parentName, parentBaseUrl, pairedAt, pairing-token SHA-256 hash), `pairing.fleetKey` (AES-256-GCM encrypted via `infra/atrest` when a cipher is provided), and `pairing.nodeId` (stable UUID, generated once on first call).
- `Status` — return the UI-facing `PairingStatus` (never exposes the fleet key or token; shows `fleetKeySet`, `discoverable`, `claimCodeActive`).
- `FleetKey` / `SetFleetKey` — decrypt/encrypt the fleet key at rest; minimum 16 characters enforced on set.
- `Discoverable` — `true` only when unpaired AND fleet key is set.
- `AnnounceInfo` — supply live node identity (NodeID, Name, Version, HTTPSPort) for the discovery responder.
- `GenerateClaimCode` — mint a short-lived (10-minute TTL) base32 uppercase claim code held in memory only; single-use (consumed on successful adopt).
- `Adopt` — validate the fleet-key assertion (`pairing.VerifyAssertion`, 60s window) + claim code under the mutex, confirm not already paired, mint a 32-byte hex pairing token, persist the locked state (token stored as SHA-256 hash only), persist the plaintext token (encrypted) in a new `pairing.enrollment` row for later use by `EnrollmentManager`, and return the token once to the caller.
- `Release` — verify the pairing token in constant time (`crypto/subtle`), clear the pairing state, and clear the `pairing.enrollment` row; idempotent when already unpaired.
- `Unpair` — admin self-drop: clear the pairing state and enrollment row; return the ex-parent's base URL so the caller can fire a courtesy notice.
- `NodeID` — return the node's stable UUID identity (generated on first call and persisted in `pairing.nodeId`).
- `ParentBaseURL` — return the bound parent's URL, or `""` when unpaired.
- `Enrollment` — return the decrypted `Enrollment` struct from the `pairing.enrollment` row (token + cert bundle), or a zero value when unset or on first adoption before enrollment.
- `SaveCert` — merge issued cert material (`NodeCertPEM`, `NodeKeyPEM`, `CARootPEM`, `CertExpiresAt`) into the stored `Enrollment`, preserving the existing token.

## Errors

| Error | HTTP mapping | Meaning |
|---|---|---|
| `ErrPairingAlreadyPaired` | 409 | Node already bound to a control plane. |
| `ErrPairingBadAssertion` | 401 | Fleet-key assertion did not verify or was stale. |
| `ErrPairingBadClaimCode` | 400 | Claim code missing, expired, or wrong. |
| `ErrPairingBadToken` | 401 | Release token does not match the stored hash. |
| `ErrPairingFleetKeyUnset` | 401 | Adopt attempted without a fleet key configured. |
| `ErrPairingFleetKeyShort` | 400 | Fleet key is fewer than 16 characters. |

## Enrollment Type

`Enrollment` is the mTLS material persisted in the `pairing.enrollment` row (AES-256-GCM encrypted via `infra/atrest`):

| Field | Description |
|---|---|
| `Token` | Plaintext pairing token (used to authenticate enroll/renew calls to the parent). |
| `NodeCertPEM` | Node leaf certificate PEM issued by the fleet CA. |
| `NodeKeyPEM` | Node private key PEM (ECDSA P-256, never sent to the parent). |
| `CARootPEM` | Fleet CA root certificate PEM (used to verify the parent's client cert on the management listener). |
| `CertExpiresAt` | Unix timestamp of certificate expiry. |

`HasCert()` returns `true` when all three PEM fields are non-empty.

## Notes

- All state lives in `RuntimeSetting` rows; the service does not own a separate table.
- The fleet key and the enrollment bundle are both encrypted at rest by the `atrest.Cipher` passed to `NewPairingService`; when `cipher` is `nil` they are stored plaintext (useful in tests or when encryption-at-rest is disabled).
- The node's stable ID (`pairing.nodeId`) is a UUID generated and persisted on first call; it survives restarts but is cleared by a factory reset (which drops and rebuilds the DB).
- Claim codes are held in memory only; they are not persisted and are lost on restart. An operator must generate a new code after restarting the node.
- `name` defaults to `os.Hostname()` when the empty string is passed to `NewPairingService`.
- `Release` and `Unpair` both call `clearEnrollment` so the mTLS material is wiped when the node leaves a pairing.
