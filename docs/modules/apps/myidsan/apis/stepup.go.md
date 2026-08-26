# Module: apps/myidsan/apis/stepup.go

## Purpose

Re-authentication endpoints and the `requireStepUp` route guard, over
`services.IStepUpService` (`services/stepup.go.md`).

## Routes

`NewStepUpApi(router, auth, stepUp, audit, guard *sharedapis.LoginGuard, trustedProxies)`
mounts `/step-up`, `auth.Middleware`-protected but **not** superadmin-gated: an ordinary
user has nothing to elevate today, but the endpoint must work for whoever the gated actions
are later extended to, and restricting it would only mean a role change could never be
re-authenticated by the person about to make one. `guard` is the **same** `LoginGuard`
instance `apis/login.go.md` and `apis/federated_auth.go.md` share, not a private one.

- `GET /api/step-up` (`status`) — `{elevated, windowSeconds}` for the caller's own session.
- `POST /api/step-up` (`verify`) — body `{password, code}`; the identity comes from the
  session claims, never the body. Checked **before** the body is read: `guardLocked(a.guard,
  r, claims.Email)` — the account key is the session's own email, so unlike the login door
  this counter cannot be aimed at an account by a stranger, only by someone already holding
  that account's cookie. A locked-out caller gets `429` + `Retry-After` at zero credential
  cost, via `login.go`'s `writeLockout` with a step-up-specific message that names the wait
  (`"too many failed re-authentication attempts — try again in N seconds"`). The wording is
  not cosmetic: the SPA renders this text verbatim inside the re-authentication modal, and
  the shared `writeLoginLockout` phrasing would tell a signed-in operator there had been
  "too many failed **login** attempts" — describing something they did not do, and sending
  them to check the front door instead of waiting the minute out. On a credential failure, records
  `services.ActionStepUpFailure` (`Outcome: denied`) — a failed step-up is a security event
  in its own right, since it is what an attacker holding only a stolen cookie would produce
  while trying to escalate — **and** now counts the attempt against the shared lockout via
  `recordThrottledFailure`, which sleeps `guard.FailedDelay()` first so the first several
  guesses are not free, then calls `guard.RecordFailure`; an attempt that engages the lockout
  also records `services.ActionLoginLockout` (`Metadata.surface: "step-up"`). Before this,
  `POST /api/step-up` was the only password-checking endpoint on the server with no lockout
  behind it — a live bench made twelve wrong guesses in 0.6s, all refused, none throttled.
  `ErrInvalidCredential`/`ErrMfaBadCode` both map to the same generic message, so this
  cannot be used to learn whether the account has a second factor enrolled. Success calls
  `guardSuccess` (clearing the counters), records `services.ActionStepUpSuccess`, and
  returns `{elevated: true, windowSeconds}`. When the second factor that cleared was a
  **recovery** code (`Verify`'s `usedRecovery` return), also records
  `services.ActionMfaRecovery` (`Metadata: {method: recovery_code, surface: "step-up"}`) —
  a break-glass code spent here is still a scarce secret gone, and the step-up-success entry
  alone looks like a routine re-authentication.

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
- Mounted from `apps/myidsan/app/app.go`'s `RegisterAppRoutes` (`app/app.go.md`), which
  passes the login-surfaces' shared `loginGuard` as `NewStepUpApi`'s new `guard` parameter.
- Covered by `apis/stepup_throttle_test.go.md`.
