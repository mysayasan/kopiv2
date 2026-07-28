# Module: apps/myidsan/apis/password_reset.go

## Purpose

The SUPERADMIN operator queue for account recovery: list pending forgot-password
requests, resolve one (issue a temporary password to hand over out-of-band), or
dismiss a bogus one. The public request endpoint (`POST /api/login/forgot`) and the
optional self-service email flow (`GET/POST /api/auth/forgot`, `GET/POST
/api/auth/reset`) live elsewhere — `apis/login.go` and `apis/federated_auth.go`
respectively; this file is reachable only by an authenticated superadmin.

## Routes

`NewPasswordResetApi` mounts `/password-reset`, gated
`auth.Middleware` + `access.Middleware` + `access.RequireSuperadmin` — the same
privilege tier as user-account management, since resolving a request is a
privilege-affecting action (it produces a working credential for the account):

- `GET /api/password-reset` (`list`) — returns the pending queue via
  `IPasswordResetService.ListPending` (see `services/password_reset.go.md`).
- `POST /api/password-reset/{id}/resolve` (`resolve`) — issues a fresh temporary
  password (`IPasswordResetService.Resolve`), stamping the caller's user ID (from
  JWT claims) as `ResolvedBy`. Returns `{temporaryPassword: "<plaintext>"}` — shown
  to the operator exactly once; the account is flagged must-change so the temporary
  password is effectively single-use. Maps `ErrResetRequestNotFound` → `404` and
  `services.ErrThirdPartyOnlyAccount` → `ErrLimitedAccess` ("this account signs in
  through an external identity provider — reset it there").
- `POST /api/password-reset/{id}/dismiss` (`dismiss`) — closes a request without
  issuing a password (spam/duplicate/mistaken submission), stamping `ResolvedBy`.
  Maps `ErrResetRequestNotFound` → `404`.

## Notes

- `requestId` parses and validates the `{id}` path var, rejecting `<= 0` with
  `ErrBadRequest`.
- Mounted from `apps/myidsan/app/app.go` alongside `apis.NewMfaApi`, sharing the same
  `passwordResetService` instance the login/federated-auth APIs use for the public
  request endpoint — a resolved request is immediately reflected the next time the
  queue is listed.
- The menu row for this page (`Id: "resetRequests"`, seeded in `app.go`'s
  `Seeders`) is matrix-gated (`SeedRbac: true`) like any other admin section, on top
  of the `RequireSuperadmin` gate applied here.
