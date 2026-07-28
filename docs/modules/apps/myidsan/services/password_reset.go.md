# Module: apps/myidsan/services/password_reset.go

## Purpose

Implements `IPasswordResetService` — myidsan's account-recovery flow. Two channels,
both reachable from the same `Request` call: an always-on operator queue (air-gap
safe; a superadmin issues a temporary password to hand over out-of-band) and an
**optional** SMTP self-service email link (active only when `infra/mailer.Mailer.Enabled()`
is true). Only **local** accounts are eligible — LDAP/Kerberos/OIDC accounts have no
password here to reset and are directed to their upstream IdP by omission (the
service silently no-ops for them).

## Responsibilities

- `Request(ctx, identifier, requestIp, origin)` — the public "forgot password" entry
  point (called from both `apis/login.go`'s `forgotPassword` and
  `apis/federated_auth.go`'s `forgotPost`). Resolves `identifier` via
  `IUserLoginService.GetByEmail`; a miss, a not-found error, or an account with no
  local password (`Userpwd == ""`, i.e. federated/SSO-only) is a **silent no-op** —
  the method returns `nil` either way. On a real local-account match it creates a
  `PasswordResetRequest` row (`Status: "pending"`) and, when mail is enabled, also
  calls `sendSelfServiceEmail`; an email send failure is swallowed (`return nil`) so
  it never blocks the caller's generic response — the operator queue entry already
  guarantees recovery. **Deliberately returns no signal about whether an account
  matched** — the caller must always respond generically (no
  account-enumeration oracle).
- `MailEnabled()` — `true` only when the wired `*mailer.Mailer` is both non-nil and
  `Enabled()` (config `smtp.enabled` + a relay host present). Exposed so the API
  layer can vary its generic confirmation wording ("an administrator has been
  notified" vs. "we've emailed a link") without ever varying on account match.
- `sendSelfServiceEmail(ctx, userId, requestId, email, origin)` — mints a random
  32-byte token (`newResetToken`, base64 URL-safe), stores a `resetTokenEntry{UserId,
  RequestId, Email}` in `cache.Store` under `pwreset:token:<token>` with a
  `resetTokenTTL` of 30 minutes, builds an absolute link
  (`origin + "/api/auth/reset?token=" + token`), and sends a plain-text email via
  `infra/mailer.go.md`.
- `ListPending(ctx)` — the operator queue: `Status == "pending"` rows, newest first
  (`RequestedAt DESC`), capped at 500.
- `Resolve(ctx, requestId, adminId)` — loads the request, calls
  `IUserLoginService.AdminResetPassword` (force-sets a fresh temporary password,
  flags must-change) on `req.UserLoginId`, marks the request resolved with
  `Channel: "admin"` and `ResolvedBy: adminId`, and returns the plaintext temporary
  password **once** to the API layer.
- `Dismiss(ctx, requestId, adminId)` — marks a request resolved with an empty
  channel and no password issued (spam/duplicate/mistaken request).
- `ResolveToken(ctx, token)` — validates a self-service token
  (`lookupToken`) and returns the bound account's email, for rendering the
  set-new-password page (`apis/federated_auth.go`'s `resetPage`) without yet
  consuming the token.
- `CompleteSelfService(ctx, token, newPassword)` — validates the token, calls
  `IUserLoginService.SetPasswordSelfService`, **consumes the token** (deletes the
  cache entry — single-use), and best-effort marks the originating request resolved
  (`Channel: "self"`, `ResolvedBy: <the user's own id>`) if it is still pending.

## Token lifetime and storage

- `resetTokenCacheKey = "pwreset:token:"`, `resetTokenTTL = 30 * time.Minute` — short
  enough to bound a leaked link, long enough for a user to act on an internal email.
- The token grants only the right to set a new password for **one** resolved
  account, **once** — `lookupToken` treats a missing/expired/malformed entry (or an
  empty `store`) identically as `ErrResetTokenInvalid`, never distinguishing "never
  existed" from "already used" from "expired" in any response (no oracle).

## Errors

`ErrResetRequestNotFound` (unknown/deleted queue row id), `ErrResetTokenInvalid`
(missing/expired/already-consumed self-service token) — surfaced to
`apps/myidsan/apis/password_reset.go` and `apps/myidsan/apis/federated_auth.go`,
which map each to the appropriate HTTP status / rendered page.
`services.ErrThirdPartyOnlyAccount` (from the underlying `IUserLoginService` calls)
also propagates from `Resolve`/`CompleteSelfService` if a request somehow points at a
non-local account.

## Security

- No account-enumeration oracle anywhere in the public path: `Request` never returns
  a match/no-match signal, and both API callers (`login.go`, `federated_auth.go`)
  render the identical generic confirmation regardless of the outcome.
- The self-service reset link **never issues a session** — `CompleteSelfService` only
  sets the password; any enrolled MFA factor is still presented at the account's next
  normal login.
- The temporary password from `Resolve` is shown to the operator exactly once (the
  API layer does not persist or log it) and forces `MustChangePassword` on the
  account, making it effectively single-use.
