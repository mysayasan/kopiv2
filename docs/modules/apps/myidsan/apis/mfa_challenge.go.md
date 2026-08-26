# Module: apps/myidsan/apis/mfa_challenge.go

## Purpose

The pre-session MFA challenge shared by both password login surfaces (the SPA's
`login.go` and the server-rendered `federated_auth.go`). When a password succeeds
but the account has a confirmed second factor, **no session cookie is issued**.
Instead an opaque, single-use, server-revocable token is minted and the client must
exchange it plus a valid code for the session. This ordering is load-bearing: a
`kopiv2_access` cookie is a federation-capable SSO session, so handing one out
before the second factor would let a password-only attacker mint relying-app
tokens at `/api/auth/authorize`.

## Responsibilities

`mfaChallenger` wraps a `cache.Store` (the same store used for session revocation),
a `services.IMfaService`, and (optionally) a `services.IWebAuthnService`, and is
nil-safe: when every dependency is absent (minimal wiring, tests) it behaves as "no MFA
configured" rather than panicking. `webauthn` is attached via `withWebAuthn(w)` rather than
a constructor parameter, so every existing call site and test keeps working unchanged and
opts in to the security-key factor explicitly. Both password login surfaces now do:
`apis/login.go.md`'s `NewLoginApi` and `apis/federated_auth.go.md`'s `NewFederatedAuthApi`
— the latter closed a real MFA bypass, since the server-rendered page is where a relying
app's SSO hop lands and an account whose only factor was a security key would otherwise
have cleared the gate with none checked at all.

- `required(ctx, userId)` — reports whether the account must clear a second factor,
  now asking **either** factor kind: a confirmed TOTP factor (`mfa.HasConfirmedFactor`)
  short-circuits true; otherwise, if a `webauthn` service is attached and enabled, a
  registered security key (`webauthn.HasCredential`) also counts. False (fail-**open** to
  password-only) only when neither is wired at all — never when a lookup errors, so a
  transient DB fault cannot silently drop the second factor (the caller propagates the
  error as a `500` instead).
- `methods(ctx, userId)` — lists which factor kinds this specific account can actually
  present (`"webauthn"` before `"totp"` — a key is both stronger and quicker to use than
  typing a code), so the login response's `mfaMethods` field lets the client prompt for
  exactly what will work rather than always assuming TOTP. Unlike `required`, a lookup
  error here is swallowed (that factor is simply omitted) — this only decides which prompt
  to show, not whether one is owed.
- `issue(ctx, r, userId)` — mints a random 32-byte token
  (`base64.RawURLEncoding`), bound to the request's client fingerprint
  (`mfaFingerprint`), and stores `mfaPendingEntry{UserId, Fingerprint, Attempts: 0}`
  under `mfa:challenge:<token>` with a `mfaChallengeTTL` (5 minute) expiry.
- `peek(ctx, r, token)` — validates a token and its client binding **without** consuming
  it, returning the account it was issued for. Added for the WebAuthn login legs
  (`apis/login.go.md`'s `webauthnLoginBegin`/`webauthnLoginFinish`): the security-key
  ceremony needs two round trips against the same token (fetch a challenge, then present
  the signed assertion), so the token can only be spent once the factor actually proves
  itself — see `consume`.
- `consume(ctx, token)` — deletes the token, making it single-use like the TOTP path's
  `redeem`. Called by `webauthnLoginFinish` only after the assertion verifies.
- `redeem(ctx, r, token, code) (userId int64, usedRecovery bool, err error)` — verifies
  `(token, code)` from a follow-up request:
  1. Unknown/expired token → `errMfaChallengeInvalid`.
  2. Client fingerprint mismatch (replayed from a different IP/User-Agent than the
     token was issued to) → the token is deleted and `errMfaChallengeInvalid` is
     returned — a captured token cannot be redeemed from a different client.
  3. `services.IMfaService.VerifyCode` (TOTP or a recovery code) — a bad code
     increments the per-token `Attempts` counter (re-persisting the entry to
     preserve it) and returns `services.ErrMfaBadCode`; once `Attempts >=
     mfaChallengeMaxTry` (5) the token is deleted outright, so a captured token
     cannot be ground against 10^6 codes.
  4. Success deletes the token (**single-use**) and returns the resolved
     `userId`, plus `MfaVerifyResult.UsedRecovery` — whether a recovery code rather than
     TOTP is what cleared it. This is the only layer that still knows which kind was spent;
     both callers (`login.go.md`'s `mfaLogin`, `federated_auth.go.md`'s `mfaPost`) use it to
     record `services.ActionMfaRecovery` separately from an ordinary sign-in.
- `mfaFingerprint(r)` — SHA-256 of `loginGuardKey(r) + "\x00" + r.UserAgent()`,
  base64-encoded; binds a challenge to the client that requested it using the
  connecting IP (never a spoofable forwarded header — reuses the same key
  `login.go`'s `LoginGuard` uses) plus the User-Agent, hashed so the cache never
  stores the raw values.

## Constants

- `mfaChallengeTTL` = 5 minutes
- `mfaChallengeMaxTry` = 5
- `mfaChallengePrefix` = `"mfa:challenge:"`
- `mfaChallengeTokLen` = 32 bytes

## Notes

- The pending cache entry carries the resolved user id only — it grants nothing on
  its own beyond the right to attempt the second factor for that one user, from
  the same client, a bounded number of times.
- Consumed by `login.go`'s `completeLoginOrChallenge`/`mfaLogin` (SPA/JSON) and
  `federated_auth.go`'s `loginPost`/`mfaPost` (server-rendered) — see those docs
  for the two HTTP surfaces built on top of this shared primitive. `login.go`'s
  `webauthnLoginBegin`/`webauthnLoginFinish` are the third consumer, added for security
  keys. `peek`/`methods` are also called from `federated_auth.go`'s
  `renderMfaChallenge` — resolved there rather than passed in, so every render path
  (including a re-render after a bad code) offers the security-key option without
  threading the user id through — but `consume` is only ever called from
  `webauthnLoginFinish`, once an assertion actually verifies; the TOTP path still spends
  its token through `redeem`.
