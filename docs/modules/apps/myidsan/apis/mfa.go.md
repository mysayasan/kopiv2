# Module: apps/myidsan/apis/mfa.go

## Purpose

The self-service and admin second-factor management surface: a signed-in user
manages their **own** factor (enroll, confirm, regenerate recovery codes, disable);
a superadmin can reset **another** user's factor for the lost-device case. The
pre-session login challenge itself lives in `login.go`/`mfa_challenge.go` (it has
no session yet) — this file never gates a login, it only manages an already-signed-
in account's factor.

## Routes

Self-service, `auth.Middleware`-protected, no RBAC matrix (mirrors
`/api/login/default/change-password` — every route acts on the caller's own
account, identified from the JWT claims):

- `GET /api/mfa` (`status`) — returns `MfaStatus` (see `services/mfa.go.md`).
- `POST /api/mfa/enroll` (`beginEnroll`) — body `{label}` (optional); stages an
  unconfirmed factor and returns the enrollment challenge (`secret`, `otpauthUri`,
  `qrPngBase64`). `ErrMfaAlreadyEnrolled` → `409` if a confirmed factor already
  exists.
- `POST /api/mfa/enroll/verify` (`confirmEnroll`) — body `{code}`; confirms the
  pending factor and returns `{recoveryCodes: [...]}` (shown once). Maps
  `ErrMfaBadCode` → `401` (and records the `enroll_failed` metric),
  `ErrMfaNotEnrolling` → `400`, `ErrMfaAlreadyEnrolled` → `409`.
- `POST /api/mfa/recovery` (`regenerateRecovery`) — body `{code}`; re-proves
  possession of the factor (`requireValidCode`) before minting a fresh recovery-
  code set — otherwise a hijacked session could quietly rotate the break-glass
  codes.
- `DELETE /api/mfa` (`disable`) — body `{password, code}`; removes the caller's own
  factor. Requires both a valid current code (`requireValidCode`) **and**, for
  local accounts, the current password (`AuthenticateDefault`) — re-proving
  possession so a hijacked session cannot silently strip the second factor. A
  third-party-only (directory/SSO) account has no local password, so
  `ErrThirdPartyOnlyAccount` from `AuthenticateDefault` is tolerated and the code
  gate alone stands.

Admin, `auth.Middleware` + `access.Middleware` + `access.RequireSuperadmin`:

- `DELETE /api/mfa-admin/{id}` (`adminReset`) — clears another user's factor
  unconditionally (no code/password re-proof — the caller is already
  superadmin-gated, the same privilege tier as role assignment). This is the
  lost-device recovery path for a non-superadmin user; for the **stock
  superadmin** specifically, see the `RESET_MFA` boot marker in
  `app/firstrun.go.md`. **Gated by `requireStepUp`** (`apis/stepup.go.md`, Phase 2) —
  clearing someone else's second factor is exactly what an attacker with a stolen cookie
  would do to take over an account, so it requires a fresh credential. On success, records
  `services.ActionMfaAdminReset` (`apis/audit.go.md`) — if it was not the account holder
  who asked for it, this entry is how they find out.

## Metrics

`MetricMfaChallengeTotal = "myidsan_mfa_challenge_total"` — counts second-factor
verification outcomes by `{result}` (`recordChallenge`): `enroll_confirmed`,
`enroll_failed`, `failed` (self-service code-gate failure), plus `issued`/
`success`/`failed`/`expired` recorded by the login-time challenge in
`login.go`/`federated_auth.go`. A spike in `failed` is the signal that matters — an
online guessing attempt against a known password. Described once via
`deps.Metrics.Describe` in `app/app.go`.

## Notes

- `mfaApi` holds no reference to the login/challenge machinery; it is constructed
  with the same `services.IMfaService` instance the login APIs use
  (`apps/myidsan/app/app.go`), so enrollment state is immediately visible to the
  next login attempt.
- `mfaApi` embeds `auditRecorder` (`apis/audit.go.md`); `NewMfaApi` now also takes
  `services.IAuditService` and `services.IStepUpService` parameters (Phase 2).
- `writeCodeGateError` centralizes the `ErrMfaBadCode`/`ErrMfaNotEnrolled`/other
  mapping used by both `regenerateRecovery` and `disable`.
