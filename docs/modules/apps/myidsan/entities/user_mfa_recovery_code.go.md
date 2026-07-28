# Module: apps/myidsan/entities/user_mfa_recovery_code.go

## Purpose

Entity for one single-use break-glass recovery code, stored only as a bcrypt hash.
A user is issued a whole set (10, see `infra/mfa.GenerateRecoveryCodes`) at
enrollment and shown them exactly once; each can substitute for a TOTP code once at
the `/api/login/mfa` (or server-rendered `/api/auth/mfa`) step.

## Fields

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `UserLoginId` | FK to the owning account. |
| `CodeHash` | `json:"-"` — bcrypt hash of the normalized code (`infra/mfa.NormalizeRecoveryCode`). Codes are high-entropy, so the login-default bcrypt cost is sufficient. |
| `UsedAt` | 0 while unused; set to the consumption time on first (and only) use. |
| `CreatedBy / CreatedAt` | Audit user ID / timestamp. |

## Notes

- This is a 1-N relation (many codes per user) — always queried with `Get` + an
  `Equal` filter on `UserLoginId`, never `dbsql.GetByForeign`, which caps at one row
  and would hide nine of every ten codes ([[getbyforeign-limit1-bug]]).
- Registered in `apps/myidsan/app/app.go`'s `Entities()` alongside `UserMfaFactor`.
- Regenerating recovery codes (`POST /api/mfa/recovery`, or an enrollment confirm
  overwriting a prior unconfirmed factor) deletes every existing row for the user
  before inserting the fresh set — old codes are never left dangling.
