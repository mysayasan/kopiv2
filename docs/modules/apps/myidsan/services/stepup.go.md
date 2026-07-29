# Module: apps/myidsan/services/stepup.go

## Purpose

Short-lived re-authentication ("step-up"): `IStepUpService` proves the caller still holds
their credential — password, plus TOTP when a factor is enrolled — before a small set of
especially sensitive actions.

## Responsibilities

- The problem: a myidsan session lasts 72 hours, and a superadmin session can assign
  roles, clear anyone's second factor, issue a temporary password for any account, and
  export or restore the entire identity store. A cookie stolen from an unlocked laptop
  therefore grants all of that for three days with no further proof of identity.
- It is **not** a second session: it is a short-lived marker attached to the session that
  already exists, keyed by session id and cache-backed (`stepup:<sessionId>`) rather than
  stored in the database — it must expire on its own, vanish when the session is revoked
  (the key is derived from the session id, which a revoked session can never present
  again), and not outlive a restart any longer than the sessions it qualifies.
- `StepUpWindow = 5 * time.Minute` — long enough to complete a batch of admin work without
  re-typing a password per click, short enough that a walk-away laptop is not still
  elevated. Same order as the MFA challenge TTL.
- `Verify(ctx, userId, email, sessionId, password, code)` re-checks the password
  (`IUserLoginService.AuthenticateDefault`) against the **session's own** identity — `email`
  and `userId` come from the server-issued session claims, never the request body, so this
  cannot be used to test other accounts' passwords from inside any authenticated session.
  When a confirmed MFA factor exists, the TOTP code is also required
  (`IMfaService.VerifyCode`) — otherwise step-up would be satisfied by a password alone,
  exactly what a phishing attacker already has. On success, sets the cache marker.
- `IsRecent(ctx, sessionId)` reports whether the session re-authenticated inside the
  window. **Fails closed**: a cache error or a missing store returns `false` (asking the
  operator to re-authenticate is an inconvenience; silently elevating every session on a
  cache blip is a breach).
- `Window()` exposes the configured lifetime so the UI can say how long an elevation lasts.

## Notes

- `NewStepUpService(users, mfa, store)` — a `nil` `store` makes `Verify` fail with "step-up
  is unavailable on this server" rather than silently treating every caller as
  re-authenticated, which would remove the control entirely.
- `ErrStepUpRequired` is defined but the actual gate lives in `apis/stepup.go.md`'s
  `requireStepUp` wrapper, which maps a failed `IsRecent` check to a distinguishable error
  code the SPA matches on to open the re-authentication prompt.
- Consumed by `apis/stepup.go.md` (status/verify routes) and, via `requireStepUp`, by
  `apis/backup.go` (`export`, `restore`), `apis/mfa.go` (`adminReset`), and
  `apis/password_reset.go` (`resolve`).
