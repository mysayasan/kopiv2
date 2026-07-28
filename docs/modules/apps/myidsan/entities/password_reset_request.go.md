# Module: apps/myidsan/entities/password_reset_request.go

## Purpose

Entity for one account-recovery request raised from the public "forgot password"
form (`POST /api/login/forgot` / server-rendered `POST /api/auth/forgot`). It backs
the always-on, air-gap-safe operator queue: a superadmin sees pending requests and
resolves one by issuing a temporary password to hand over out-of-band. When the
optional SMTP self-service link is configured and used instead, the row is marked
resolved with `Channel = "self"` without any operator action.

A row is created **only** when the submitted identifier resolves to an existing
**local** account (LDAP/Kerberos/OIDC accounts have no password here to reset and
are silently excluded) — but the public request endpoint always returns the same
generic response regardless, so the row's existence is never observable to the
requester (no account-enumeration oracle; see
`apps/myidsan/services/password_reset.go.md`).

## Fields

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `UserLoginId` | FK to the resolved local account. |
| `Email` | The resolved account email, shown to the operator in the queue. |
| `Status` | `"pending"` until an operator resolves or dismisses it, then `"resolved"`. |
| `Channel` | How it was resolved: `"admin"` (operator issued a temp password) or `"self"` (user completed the SMTP self-service link). Empty while pending. |
| `RequestIp` | The connecting peer at request time — operator abuse/spam context. |
| `RequestedAt` | Unix timestamp of the original request. |
| `ResolvedBy` | The resolving superadmin's user ID (0 for a self-service resolution). |
| `ResolvedAt` | Unix timestamp of resolution/dismissal. |

## Notes

- Registered in `apps/myidsan/app/app.go`'s `Entities()` for bootstrap schema
  generation, alongside `UserMfaFactor`/`UserMfaRecoveryCode`.
- Owned exclusively by `apps/myidsan/services/password_reset.go` (`IPasswordResetService`);
  the superadmin queue API (`apps/myidsan/apis/password_reset.go`) only reads/updates
  through that service, never the repo directly.
- Listed via `Get` filtered `Status == "pending"`, sorted `RequestedAt DESC` — there is
  no separate index/table for resolved history browsing today; resolved rows remain in
  the table but are not surfaced by `ListPending`.
