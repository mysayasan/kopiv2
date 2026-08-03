# Module: apps/myidsan/services/mfa.go

## Purpose

Implements `IMfaService` — myidsan's TOTP second-factor lifecycle: self-serve
enrollment, per-login verification (TOTP or a single-use recovery code), and
teardown. This is the only place `UserMfaFactor`/`UserMfaRecoveryCode` rows are
read or written; `infra/mfa` (see `docs/modules/infra/mfa/`) supplies the
protocol-level primitives (secret generation, code validation, QR rendering) but
has no knowledge of storage or at-rest sealing.

## Responsibilities

- `Status(ctx, userId)` — non-sensitive `MfaStatus` projection (`enrolled`,
  `confirmedAt`, `label`, `recoveryRemaining`) for the owning user's Security page.
  Never carries the secret or the recovery codes.
- `HasConfirmedFactor(ctx, userId)` — the login-gate predicate consumed by
  `apps/myidsan/apis/mfa_challenge.go`'s `mfaChallenger.required`: does this
  account require a second factor before a session is issued?
- `BeginEnroll(ctx, userId, accountEmail, label)` — generates a fresh TOTP secret
  (`infra/mfa.GenerateSecret`), seals it (`encodeSecret`/`atrest.Cipher`), and
  stages an **unconfirmed** `UserMfaFactor` row (`ConfirmedAt = 0`). Refuses with
  `ErrMfaAlreadyEnrolled` if a *confirmed* factor already exists (delete it first);
  a prior *unconfirmed* staging factor is overwritten with the fresh secret rather
  than accumulating rows. Returns `MfaEnrollChallenge{Secret, OtpauthUri,
  QrPngBase64}` — the QR is rendered server-side (`infra/mfa.QRPNG`) and returned
  base64-encoded.
- `ConfirmEnroll(ctx, userId, code)` — validates the first code
  (`infra/mfa.Validate`) against the pending factor, marks it confirmed
  (`ConfirmedAt`, `LastStep`, `LastUsedAt`), and mints the one-time recovery-code
  set via `replaceRecoveryCodes` (returned in plaintext once — never persisted
  unhashed).
- `VerifyCode(ctx, userId, code)` — the single verification path used by both the
  pre-session login challenge (`apis/mfa_challenge.go`) and self-service teardown
  (`disable`/`regenerateRecovery`). Tries TOTP first (cheapest, common case;
  advances `LastStep`/`LastUsedAt` on success), then falls back to
  `consumeRecoveryCode` — a single-use recovery code match.
- `RegenerateRecovery(ctx, userId)` — issues a fresh recovery-code set via
  `replaceRecoveryCodes`, invalidating the old one. Refuses with
  `ErrMfaNotEnrolled` if the account has no confirmed factor.
- `Disable(ctx, userId)` — removes the user's factor row and all recovery codes.
  Used both for self-service teardown (after the API layer re-proves possession)
  and for the superadmin admin-reset / `RESET_MFA` boot-marker lost-device paths,
  neither of which re-proves possession themselves (see "Security" below).

## Recovery-code helpers

- `loadRecoveryCodes` / `countUnusedRecovery` / `deleteRecoveryCodes` /
  `replaceRecoveryCodes` — CRUD over `UserMfaRecoveryCode`, always `Get` + an
  `Equal` filter on `UserLoginId` (never `dbsql.GetByForeign`, which caps at one
  row — see `entities/user_mfa_recovery_code.go.md`).
- `consumeRecoveryCode(ctx, userId, code)` — normalizes the input
  (`infra/mfa.NormalizeRecoveryCode`), then bcrypt-compares against every unused
  code for the user, marking the first match's `UsedAt`. Returns `false` (not an
  error) when nothing matches.

## At-rest secret wrapping

`encodeSecret`/`decodeSecret` mirror `services/directory.go`'s handling of the LDAP
bind password: `cipher` (an `*infra/atrest.Cipher`, possibly `nil` when
`security.encryptAtRest` is off) seals the plaintext base32 TOTP secret before it is
persisted in `SecretEnc`, and unseals it (`atrest.IsEncrypted` gate, falling back to
the raw stored value on any decode failure) on read. `NewMfaService`'s `cipher`
parameter may be `nil` — the secret is then stored as-is, matching the suite-wide
`atrest` fallback.

## Configuration

| Constant | Value |
|---|---|
| `mfaFactorKind` | `"totp"` — the only kind this table holds. WebAuthn security keys are a second MFA factor kind, but they are many-per-user with a different shape (public key, signature counter, transports), so they live in their own table/service (`entities/user_webauthn_credential.go`, `services/webauthn.go`) rather than a second `Kind` row here. |
| `issuer` (constructor param) | Defaults to `"myidsan"` when blank; used in `infra/mfa.OtpauthURI`. |

## Errors

`ErrMfaAlreadyEnrolled`, `ErrMfaNotEnrolling` (no pending enrollment to confirm),
`ErrMfaNotEnrolled` (no confirmed factor), `ErrMfaBadCode` — surfaced to the API
layer (`apps/myidsan/apis/mfa.go`, `apps/myidsan/apis/mfa_challenge.go`,
`apps/myidsan/apis/login.go`, `apps/myidsan/apis/federated_auth.go`), which maps
each to the appropriate HTTP status.

## Security

- The shared secret is never returned by any API after the initial enrollment
  challenge, and recovery codes are shown in plaintext exactly once (at
  `ConfirmEnroll`/`RegenerateRecovery`) — only bcrypt hashes are persisted.
- `VerifyCode` and the recovery-code comparison do not short-circuit on the first
  candidate in a way that would leak timing beyond what bcrypt/HMAC constant-time
  comparison already bounds.
- `Disable` is a privilege-affecting action wherever it is reachable: self-service
  (`DELETE /api/mfa`) requires the API layer to re-prove possession (a valid code,
  plus the current password for local accounts) before calling it; the superadmin
  admin-reset route (`DELETE /api/mfa-admin/{id}`) and the boot-time `RESET_MFA`
  marker call it directly, by design, as the documented lost-device escape hatches.
