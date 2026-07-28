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

`mfaChallenger` wraps a `cache.Store` (the same store used for session revocation)
and a `services.IMfaService`, and is nil-safe: when either dependency is absent
(minimal wiring, tests) it behaves as "no MFA configured" rather than panicking.

- `required(ctx, userId)` — reports whether the account must clear a second factor.
  False (fail-**open** to password-only) when MFA is not wired at all — but never
  when the underlying lookup errors, so a transient DB fault cannot silently drop
  the second factor (the caller propagates the error as a `500` instead).
- `issue(ctx, r, userId)` — mints a random 32-byte token
  (`base64.RawURLEncoding`), bound to the request's client fingerprint
  (`mfaFingerprint`), and stores `mfaPendingEntry{UserId, Fingerprint, Attempts: 0}`
  under `mfa:challenge:<token>` with a `mfaChallengeTTL` (5 minute) expiry.
- `redeem(ctx, r, token, code)` — verifies `(token, code)` from a follow-up
  request:
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
     `userId`.
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
  for the two HTTP surfaces built on top of this shared primitive.
