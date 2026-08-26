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
  `ErrMfaNotEnrolling` → `400`, `ErrMfaAlreadyEnrolled` → `409`. On success, records
  `services.ActionMfaEnroll` (`Metadata: {recoveryCodes: <count>}`) — recorded at
  **confirmation**, not at `beginEnroll`: staging a secret changes nothing about how the
  account authenticates, whereas this is the moment the account starts owing a second
  factor, and the trail needs to be able to date it against a later removal.
- `POST /api/mfa/recovery` (`regenerateRecovery`) — body `{code}`; re-proves
  possession of the factor (`requireValidCode`) before minting a fresh recovery-
  code set — otherwise a hijacked session could quietly rotate the break-glass
  codes. Records `services.ActionMfaRegenerate` (`Metadata: {recoveryCodes: <count>}`) on
  success — rotating the set invalidates every code the account holder has written down, and
  the old hashes are gone afterwards, so there is nothing else to compare against. If the
  code that re-proved possession was itself a recovery code, also records
  `services.ActionMfaRecovery` (`surface: "recovery-regenerate"`, via `recordRecoveryBurn`).
- `DELETE /api/mfa` (`disable`) — body `{password, code}`; removes the caller's own
  factor. Requires both a valid current code (`requireValidCode`) **and**, for
  local accounts, the current password (`AuthenticateDefault`) — re-proving
  possession so a hijacked session cannot silently strip the second factor. A
  third-party-only (directory/SSO) account has no local password, so
  `ErrThirdPartyOnlyAccount` from `AuthenticateDefault` is tolerated and the code
  gate alone stands. Checks `selfThrottleLocked` first and calls `selfThrottleFailure`
  on a wrong password (`apis/login.go.md`'s "Self-Throttled Password Re-checks") —
  defence in depth only here, since the code gate is checked before the password and
  already refuses an attacker with no second factor; still throttled rather than
  relying on that ordering never changing. On success, records `services.ActionMfaDisable` — the single most
  important line this trail can hold: `Disable` deletes the factor row and every
  recovery-code hash, so the act erases its own evidence and afterwards the account is
  indistinguishable from one that never enrolled. If the code that authorized the removal
  was a recovery code, also records `services.ActionMfaRecovery` (`surface: "mfa-disable"`)
  — someone dismantling the second factor without the authenticator itself is a materially
  different event from the routine case.

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
- `mfaApi` gained a `guard *sharedapis.LoginGuard` field; `NewMfaApi` gained a trailing
  `guard` parameter (same `*sharedapis.LoginGuard` instance `apis.NewLoginApi` and
  `apis.NewWebAuthnApi` share) so `disable` can throttle its password re-check — see
  `apis/login.go.md`'s "Self-Throttled Password Re-checks".
- `mfaApi` embeds `auditRecorder` (`apis/audit.go.md`); `NewMfaApi` now also takes
  `services.IAuditService` and `services.IStepUpService` parameters (Phase 2).
- `writeCodeGateError` centralizes the `ErrMfaBadCode`/`ErrMfaNotEnrolled`/other
  mapping used by both `regenerateRecovery` and `disable`.
- `requireValidCode(r, userId, code) (usedRecovery bool, err error)` now reports whether a
  **recovery** code (vs. TOTP) satisfied the gate, sourced from `IMfaService.VerifyCode`'s
  `MfaVerifyResult.UsedRecovery` — the caller then decides whether to record a burn.
  `recordRecoveryBurn(r, userId, surface)` writes the `services.ActionMfaRecovery` entry;
  `surface` (`"recovery-regenerate"` / `"mfa-disable"`) distinguishes what the same burn
  means depending on where it happened.
- Covered by `apis/mfa_audit_test.go.md` — a live bench found five declared audit actions
  (`mfa.enroll`, `mfa.disable`, `mfa.recovery_used`, `mfa.recovery_regenerate`,
  `login.mfa_challenge`) never written by anything; this file, `apis/login.go.md`,
  `apis/mfa_challenge.go.md`, `apis/federated_auth.go.md`, and `apis/webauthn.go.md` now
  emit all five.
