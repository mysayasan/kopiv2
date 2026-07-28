# Module: apps/myidsan/entities/user_mfa_factor.go

## Purpose

Entity for one enrolled second factor on a myidsan-verified account (local
password, or LDAP/AD replayed through myidsan). Stored in its own table rather than
as columns on `UserLogin` because `SecretEnc` is sealed at rest (`infra/atrest`) and
must never ride along in a `UserLogin` projection — the same discipline that keeps
the password hash out of user listings.

## Fields

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `UserLoginId` | FK to the owning account. |
| `Kind` | `"totp"` today; `"webauthn"` reserved for later. A user may hold one factor per kind. |
| `SecretEnc` | `json:"-"` — the `infra/atrest`-sealed, base64-wrapped TOTP shared secret. Never returned by any API, not even to the owning user after enrollment completes. |
| `Label` | The device name the user typed at enrollment ("Pixel 8"). |
| `ConfirmedAt` | 0 until the user proves one code. An unconfirmed factor never gates a login and is overwritten wholesale by a fresh `BeginEnroll`. |
| `LastStep` | The last accepted TOTP time-step — the replay guard (see `infra/mfa.Validate`). Any code whose step is ≤ `LastStep` is refused. |
| `CreatedBy / UpdatedBy` | Audit user IDs. |
| `CreatedAt / UpdatedAt` | Audit timestamps (Unix). |
| `LastUsedAt` | Unix timestamp of the last successful verification. |

## Notes

- Lookups filter on `UserLoginId` with an `Equal` filter (`Get`), never
  `dbsql.GetByForeign` — that generic helper silently returns only one child row,
  which would break once a second `Kind` (WebAuthn) is added per user
  ([[getbyforeign-limit1-bug]]).
- Registered in `apps/myidsan/app/app.go`'s `Entities()` for bootstrap schema
  generation, alongside `UserMfaRecoveryCode`.
- Owned exclusively by `apps/myidsan/services/mfa.go` (`IMfaService`); no other
  service reads or writes this table.
