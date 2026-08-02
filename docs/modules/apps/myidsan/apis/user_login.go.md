# Module: apps/myidsan/apis/user_login.go

## Purpose

REST API endpoints for user login credential management.

## Route Group

Base path: `/api/user-credential`

- `GET /api/user-credential`
- `GET /api/user-credential/email`
- `POST /api/user-credential`
- `PUT /api/user-credential`
- `DELETE /api/user-credential/{id}`

## Middleware Contract

Protected by auth middleware + `AccessSessionMidware` + `RequireSuperadmin`. The entire `/api/user-credential` surface is **superadmin-only** — role assignment is a privilege-escalation vector and must not be reachable by any non-superadmin role regardless of matrix grants.

## Handler Behavior

- `post` is the admin-provisioning create path — a role is assigned up front in the
  request body (`userRoleId`), unlike self-registration
  (`/api/login/default/register`), which always lands the new account pending
  (`UserRoleId: 0`). Requires non-empty `email`/`userpwd`; forces `id = 0`,
  `isActive = true`, defaults `createdAt` to now, and stamps `createdBy` from the
  caller's JWT claims when present. Still gated by the surface-wide
  `RequireSuperadmin` middleware below. Used today by the SPA's setup wizard
  ("create your own superadmin" step,
  `views/react-webpack/src/views/components/setup.js`) and is available for the
  Users admin page as well. The password is now checked against the configured
  policy (`services.ValidatePassword`, Productization Phase 3 — see
  `services/password_policy.go.md`) before the account is created; previously this
  was the one password-setting path with no strength check at all beyond non-empty.
- GET supports `limit`, `offset`, `filters`, and `sorters` query parameters.
- Filter and sorter query values use the shared SQL enum JSON contract from `query_options.go`.
- Read handlers return myidsan output DTOs through `IUserLoginDtoService`. The output DTO carries no password field, so GET responses (including the account list) never include a stored bcrypt hash.
- PUT decodes the myidsan input DTO, then projects it to a `UserLogin` entity for service writes.
- PUT rejects unknown JSON fields.
- PUT blocks self-role-change: if `body.Id` matches `claims.Id` and `body.UserRoleId` differs from `claims.RoleId`, the request is rejected with 403. This prevents a superadmin from accidentally (or maliciously) demoting or escalating their own role.
- `/email` uses the `email` query parameter for exact unique lookup.
- DELETE parses `{id}` from route params.

## Session Termination on Disable/Delete (Phase 2)

`endSessionsFor(r, userId, reason)` calls `services.ISessionService.RevokeAllForUser` for
every live session belonging to a user, called from `put` when `!body.IsActive` (an
account just disabled) and unconditionally from `delete`. Before this, disabling an
account did not sign anybody out: the auth middleware only validates the cached session
entry, which carries no account-status flag, so an already signed-in user kept working
until their session expired — up to 72 hours after an administrator believed access was
cut off. (RBAC-gated routes did start refusing them, since the role resolver reports the
account disabled — but auth-only routes such as `/api/profile/*` and `/api/mfa` stayed
reachable.) When at least one session was actually ended, an audit entry
(`services.ActionSessionRevokeAll`, `Detail: "sessions ended because the account was
<disabled|deleted>"`) is written via `m.audit`.

`recordUserAudit(r, action, targetId, targetEmail, meta)` also records
`services.ActionUserUpdate` (with `{isActive, roleId}` metadata) from `put` and
`services.ActionUserDelete` from `delete`.

`userLoginApi` now also carries `sessions services.ISessionService`, `audit
services.IAuditService`, `trusted []*net.IPNet` (parsed from the shared
`trustedProxies` config via `middlewares.ParseTrustedProxies`), and (Productization
Phase 3) `policy config.EffectivePasswordPolicy`; `NewUserLoginApi`'s signature gained
matching trailing parameters, `policy` inserted before `trustedProxies`.

## Second-Factor Cleanup on Delete

`NewUserLoginApi` gained two more trailing parameters, `mfa services.IMfaService` and
`webauthn services.IWebAuthnService` (both optional — a nil one simply skips that factor
kind), used **only** by `removeFactorsFor(r, userId)`, called from `delete` right after
`endSessionsFor`:

- `mfa.Disable(ctx, userId)` removes the account's TOTP factor + recovery codes. For TOTP
  an orphaned row would have been harmless (lookups are always by `UserLoginId`), so this
  closes a smaller gap than the one below — mostly hygiene.
- `webauthn.DeleteAllForUser(ctx, userId)` removes every enrolled security key. This one
  **is** load-bearing: `UserWebauthnCredential.CredentialId` is unique across the **whole**
  table, not per user (see `entities/user_webauthn_credential.go.md`), so an orphaned row
  left behind by a deleted account would silently refuse to let that same physical key be
  re-enrolled later — a re-created account (or the same physical person, re-onboarded)
  would find their own hardware key rejected with no visible reason.

Both calls are **best-effort and logged, not fatal**: the account row is already gone by
the time `removeFactorsFor` runs, so failing the HTTP response over a factor-cleanup error
would report a failure for a deletion that already succeeded. Deliberately **not** called
from `put`'s disable path (`!body.IsActive`) — a disabled account still exists and may be
re-enabled, and it would need its factors again if it is.
