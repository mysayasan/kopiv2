# Module: apps/myidsan/apis/stepup.go

## Purpose

Re-authentication endpoints and the `requireStepUp` route guard, over
`services.IStepUpService` (`services/stepup.go.md`).

## Routes

`NewStepUpApi` mounts `/step-up`, `auth.Middleware`-protected but **not**
superadmin-gated: an ordinary user has nothing to elevate today, but the endpoint must
work for whoever the gated actions are later extended to, and restricting it would only
mean a role change could never be re-authenticated by the person about to make one.

- `GET /api/step-up` (`status`) — `{elevated, windowSeconds}` for the caller's own session.
- `POST /api/step-up` (`verify`) — body `{password, code}`; the identity comes from the
  session claims, never the body. On failure, records `services.ActionStepUpFailure`
  (`Outcome: denied`) — a failed step-up is a security event in its own right, since it is
  what an attacker holding only a stolen cookie would produce while trying to escalate.
  `ErrInvalidCredential`/`ErrMfaBadCode` both map to the same generic message, so this
  cannot be used to learn whether the account has a second factor enrolled. Success records
  `services.ActionStepUpSuccess` and returns `{elevated: true, windowSeconds}`.

## `requireStepUp`

`requireStepUp(stepUp services.IStepUpService, next http.HandlerFunc) http.HandlerFunc`
guards one sensitive handler at a time — a plain wrapper, not middleware, since it is
applied to individual routes (not whole subrouters: most of what a superadmin does does
not warrant re-typing a password). A `nil` service makes the guard inert (a deliberate
choice for an install with no cache — the alternative would permanently wall off the
affected admin actions). Otherwise it checks `IsRecent` against the caller's session id and,
on failure, sends `ErrLimitedAccess` with the `step_up_required` sentinel in the message
body — the SPA matches on that string to open the re-authentication prompt (`SendError` has
no structured-detail channel).

Applied to: `POST /api/backup/export`, `POST /api/backup/restore` (`apis/backup.go.md`),
`DELETE /api/mfa-admin/{id}` (`apis/mfa.go.md`), `POST /api/password-reset/{id}/resolve`
(`apis/password_reset.go.md`).

## Notes

- `stepUpErrorCode = "step_up_required"` is the sentinel string the SPA looks for.
- Shares `callerClaims` with `apis/session.go.md`.
- Mounted from `apps/myidsan/app/app.go`'s `RegisterAppRoutes` (`app/app.go.md`).
