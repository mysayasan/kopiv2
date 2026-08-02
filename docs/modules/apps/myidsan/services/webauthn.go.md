# Module: apps/myidsan/services/webauthn.go

## Purpose

Implements `IWebAuthnService` — FIDO2 / WebAuthn security keys as a **second factor kind
alongside TOTP** (`services/mfa.go.md`), not a replacement for it. A user may hold either
or both; the login gate (`apis/mfa_challenge.go.md`'s `mfaChallenger`) asks both services
the same question ("does this account owe a second factor?") and combines the answer,
rather than one service pretending to be the other.

Why this sits beside `IMfaService` instead of inside it: a TOTP factor is ONE row holding a
symmetric secret that must be sealed at rest, verified by a code comparison. A WebAuthn
credential is MANY rows holding public keys that need no sealing, verified by a signature
check bound to the request's origin. The private key never leaves the authenticator — a
database read of myidsan's whole store yields every TOTP secret (sealed, but the key lives
on the same host) and **no** WebAuthn signing key, which is the security argument for
preferring a key over TOTP.

## Responsibilities

- `Enabled()` — whether the feature is switched on for this install (`repo`/`store`
  non-nil and the underlying `infra/webauthn.Authority` enabled).
- `List(ctx, userId)` — the account's enrolled keys, as `WebAuthnCredentialView` (no public
  key — useless to a client and only invites someone to try trusting it).
- `HasCredential(ctx, userId)` — the login-gate predicate consumed by `mfaChallenger.required`
  and `.methods`. Asked on every password login, so it fails **closed** on a lookup error
  (a transient DB fault must not read as "this account has no second factor").
- `BeginEnroll`/`FinishEnroll` — the registration ceremony. `BeginEnroll` sends the user's
  existing credential IDs as **exclusions** so the same authenticator cannot be enrolled to
  the account twice (the browser reports `InvalidStateError`), and caps enrolment at
  `webauthnMaxKeys` (10) — generous enough for "one on the keyring, one in the safe, one at
  the office", bounded so the exclusion list sent to the browser cannot grow without limit.
  `FinishEnroll` verifies the attestation response and persists a new
  `UserWebauthnCredential` row (`entities/user_webauthn_credential.go.md`).
- `BeginAssert`/`FinishAssert` — the per-login assertion ceremony, used both by the
  self-service Profile flow is not part of this (assertion is login-only) and by the
  pre-session login legs (`apis/login.go.md`'s `webauthnLoginBegin`/`webauthnLoginFinish`).
  `FinishAssert` returns `(proven bool, note string, err error)`: `note` is a non-fatal
  anomaly (today, only a non-advancing signature counter) the caller should audit rather
  than a reason to refuse — see "Clone detection" below.
- `Rename`/`Delete`/`DeleteAllForUser` — key management. **Deliberately do NOT require
  `Enabled()`** — switching the feature off is exactly what someone does after a key is
  lost, and being unable to revoke it at that moment is the wrong failure. Enrolment and
  assertion are gated; teardown is not.

## Ceremony state

A WebAuthn ceremony is stateful — a server-generated challenge must be matched to the
response that signs it — but the state is short-lived and single-use, so it lives in
`cache.Store` under `webauthn:sess:<stateKey>` (`webauthnCeremony{UserId, Session}`), not a
table, mirroring the pre-session MFA challenge (`apis/mfa_challenge.go.md`). `takeCeremony`
loads **and deletes** the entry in one call (single-use, so a captured response cannot be
replayed), checking `entry.UserId == userId` so a response cannot be redirected onto a
different account than the challenge was issued for. The delete happens **before** the
ownership check on purpose: a wrong-user response has already failed, and leaving the
challenge alive only helps someone probing; it is not a way to destroy another user's
in-flight ceremony either, since no caller can address someone else's `stateKey` (the
enrolment key is built from the caller's own claims server-side, and the login key is the
unguessable challenge token).

- `stateKey` for enrolment: `apis/webauthn.go.md`'s `enrollStateKey(userId)` —
  `"enroll:<userId>"`, self-cleaning (a second concurrent enrolment simply replaces the
  first).
- `stateKey` for login: `apis/login.go.md`'s `webauthnLoginStateKey(mfaToken)` —
  `"login:<mfaToken>"`, so two concurrent sign-in attempts for the same account cannot
  consume each other's ceremony.

## Clone detection

`FinishAssert` matches the signing credential back to its stored row and advances its
`SignCount`. A counter that fails to advance (`CloneWarning` from the library, or a
non-zero-but-not-greater presented count) is the spec's documented clone signal — but most
platform authenticators and every synced passkey legitimately report `0` forever, making
that signal ambiguous by design rather than proof. The assertion is **accepted anyway**;
`CloneWarning` is set on the row and a `note` is returned so the caller (`apis/login.go.md`'s
`webauthnLoginFinish`) writes `services.ActionWebAuthnClone` to the audit trail. The
password was already checked before this point, so the flag is a lead to investigate, not
the only guard. Refusing would lock users out of working hardware on an ambiguous signal.

A row-update failure while persisting the new counter does **not** fail the login — the
assertion itself already verified, so failing the sign-in over a bookkeeping write would be
the wrong trade; the failure is folded into the note instead.

## Errors

`ErrWebAuthnDisabled`, `ErrWebAuthnNoCeremony` (no pending ceremony, or it expired),
`ErrWebAuthnNotFound` (key id doesn't belong to the caller), `ErrWebAuthnNoKeys` (an
assertion was requested but the account holds none) — surfaced to the API layer
(`apis/webauthn.go.md`, `apis/login.go.md`), which maps each to the appropriate HTTP status.

## Notes

- `NewWebAuthnService(repo, store, authority, timeout)` — a **nil `store` disables the
  feature outright** rather than degrading silently: without somewhere to keep the
  challenge between the two ceremony legs there is no ceremony to run, and pretending
  otherwise would accept an assertion against a challenge nobody issued.
- `authority` is `*infra/webauthn.Authority` (see `infra/webauthn/webauthn.go.md`) — the
  thin wrapper around `github.com/go-webauthn/webauthn` that resolves the Relying Party ID
  and allowed origin from the request.
- `ownedRow` confirms a credential belongs to the caller **before** any rename/delete —
  checked here, not trusted from the route, so a caller cannot act on someone else's key by
  guessing a row id.
- `loadRows`/`buildUser` skip a row that fails to base64-decode rather than failing the
  whole ceremony — one corrupt row must not lock a user out of the other keys they hold.
- `WebAuthnID()` (the library's user-handle) is derived from the row id, big-endian —
  stable for the lifetime of the account by definition, and opaque enough (a user handle is
  not secret, it just must not be a guessable personal identifier).
- Constructed and wired in `apps/myidsan/app/app.go`'s `RegisterAppRoutes` — see
  `apps/myidsan/app/app.go.md`. Mounted via `apis.NewWebAuthnApi` (`apis/webauthn.go.md`)
  and consumed by `apis.NewLoginApi` (`apis/login.go.md`) for the pre-session legs.
