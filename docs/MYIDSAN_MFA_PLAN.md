# myidsan MFA Plan (TOTP + recovery codes)

**Status: PLANNED, nothing built.** Prerequisite work (enterprise SSO P0–P3) is built
and live-benched — see `docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md`.

Goal: give myidsan a second factor for the credentials **myidsan itself verifies**, so a
stolen or phished password is no longer sufficient to reach the suite. myidsan is the
single door to NVR footage (mymatasan), fleet control (myseliasan) and IoT actuation
(myiotsan) — lamps, blinds, breakers. Password-only is the weakest link in the whole
product line.

Non-goals: SMS/email OTP (needs egress; violates [[myseliasan-intranet-airgap]] and is
phishable anyway), WebAuthn/passkeys (Phase 4 — real, but bigger: needs HTTPS origin
binding and a credential store), MFA for upstream-federated logins (Kerberos/OIDC — the
IdP owns that; see §5).

---

## Current state (verified 2026-07-22)

- **Zero MFA code anywhere in the repo** — no TOTP, HOTP, WebAuthn, passkey or
  recovery-code implementation (grepped across `apps/`, `domain/`, `infra/`).
- Every login path funnels into ONE session issuer: `issueSessionCookies`
  (`apps/myidsan/apis/login.go:648`), called by `issueLocalSession` (local + LDAP),
  the Kerberos handler, and `setOAuthSession` (Google/GitHub/OIDC).
- A per-IP lockout already exists (`sharedapis.LoginGuard`, wired at
  `apps/myidsan/app/app.go:338`) and is applied to local and LDAP logins.
- The "forced password change" gate is a **post-session** block: cookies are issued,
  then `AccessSessionMidware` rejects with `password_change_required`
  (`domain/utils/middlewares/access_rbac.go:78`). **MFA must NOT copy this pattern** —
  see §2.
- Secrets-at-rest infra exists and myidsan already uses it for the LDAP bind password
  (`infra/atrest`, key at `<data>/secret/atrest.key`).
- `UserLogin` (`domain/entities/user_login.go`) has `MustChangePassword`,
  `SsoProvider`, `SsoSubject`. No MFA columns.

---

## 1. Schema — a separate table, not columns on UserLogin

Two new myidsan entities (registered in `apps/myidsan/app/app.go` `Entities()`; the
bootstrap auto-adds missing columns from struct fields):

```go
// UserMfaFactor is one enrolled second factor. Separate table because the secret
// is encrypted at rest and must never ride along in a UserLogin projection.
type UserMfaFactor struct {
    Id           int64  // pkey
    UserLoginId  int64  // fkey1
    Kind         string // "totp" today; "webauthn" later
    SecretEnc    string // infra/atrest-sealed TOTP shared secret — NEVER returned by any API
    Label        string // device label the user typed ("Pixel 8")
    ConfirmedAt  int64  // 0 until the user proves one code; unconfirmed factors never gate a login
    LastStep     int64  // last accepted TOTP time-step — replay guard (see §3)
    CreatedAt    int64
    LastUsedAt   int64
}

// UserMfaRecoveryCode is one single-use break-glass code, stored hashed.
type UserMfaRecoveryCode struct {
    Id          int64  // pkey
    UserLoginId int64  // fkey1
    CodeHash    string // bcrypt — codes are high-entropy, so cost can stay at the login default
    UsedAt      int64  // 0 = unused
    CreatedAt   int64
}
```

**Gotcha:** look these up with `Get` + an Equal filter on the FK, **never**
`dbsql.GetByForeign` — it silently returns only one child row
([[getbyforeign-limit1-bug]]). Recovery codes are 1-N; that bug would make 9 of 10 codes
invisible.

## 2. The login protocol — MFA must be PRE-session

The must-change gate issues cookies first and blocks afterwards. MFA cannot: a valid
`kopiv2_access` cookie is a **federation-capable SSO session**, so handing one out before
the second factor would let an attacker with only the password walk straight to
`/api/auth/authorize` and mint access tokens for mymatasan/myseliasan/myiotsan. The
second factor has to come before any cookie exists.

```
POST /api/login/default            (or /api/login/ldap)
  password OK, no confirmed factor -> 200 {ok:true} + session cookies      (unchanged)
  password OK, confirmed factor    -> 200 {mfaRequired:true, mfaToken:"<opaque>"}
                                      NO cookies set
POST /api/login/mfa  {mfaToken, code}
  code OK                          -> 200 {ok:true} + session cookies
  code bad                         -> 401, mfaToken survives, attempt counted
```

`mfaToken` is an opaque random token (not a JWT — it must be server-revocable and
single-use), held in the **existing cache Store** already used for session revocation
(`domain/utils/middlewares/auth.go:27`), with:

- TTL ≤ 5 minutes,
- bound to `userLoginId` + a client fingerprint (IP + User-Agent hash) — rejected if
  either changes,
- deleted on success, on exhausted attempts, and on TTL,
- a hard per-token attempt counter (5) **independent** of the per-IP LoginGuard.

The pending state carries the resolved user id only. It grants nothing on its own.

## 3. TOTP verification rules (where MFA implementations usually go wrong)

New package `infra/mfa` (pure Go, stdlib `crypto/hmac` + `encoding/base32`; RFC 6238
is ~80 lines and protocol-frozen — no dependency needed for the algorithm itself):

- SHA-1, 6 digits, 30-second step (what every authenticator app defaults to).
- Accept a **±1 step** skew window, no wider. Wider windows multiply the brute-force
  surface for no real gain.
- **Replay guard**: persist `LastStep`; refuse any code whose step ≤ `LastStep`. Without
  this, a code shoulder-surfed or captured in a proxy log is reusable for 30–90s.
- **Constant-time compare** (`hmac.Equal`) on the digits.
- Rate limit verification **per user**, not just per IP — the attacker already holds the
  password, and 6 digits is 10^6. The per-token counter in §2 plus a LoginGuard entry
  keyed on the user id covers both a single-token grind and token-recycling.
- Enrollment secret: 20 random bytes from `crypto/rand`, base32 (no padding).

QR rendering: emit the standard `otpauth://totp/<issuer>:<email>?secret=...&issuer=...`
URI, and render the QR **server-side as a PNG** with a pure-Go encoder
(`github.com/skip2/go-qrcode`, one small dep) — an air-gapped install cannot pull a QR
library from a CDN ([[myseliasan-intranet-airgap]]). Always show the base32 secret as a
manual-entry fallback.

## 4. Enrollment & recovery

```
POST   /api/mfa/enroll        -> {secret, otpauthUri, qrPngBase64}   (creates UNCONFIRMED factor)
POST   /api/mfa/enroll/verify -> {recoveryCodes:[10]}                (confirms it; codes shown ONCE)
GET    /api/mfa               -> {enrolled, confirmedAt, label, recoveryCodesRemaining}
DELETE /api/mfa               -> removes own factor (requires current password + a valid code)
POST   /api/mfa/recovery      -> regenerate codes (invalidates old set)
```

All are `auth.Middleware`-protected (acting on the caller's OWN account). Deleting a
factor must re-prove possession — otherwise a hijacked session silently strips MFA.

Recovery codes: 10 codes, ~80 bits each, single-use, bcrypt-hashed, displayed exactly
once. A recovery code is accepted at the `/api/login/mfa` step in place of a TOTP code.

**Admin reset** (`DELETE /api/user-credential/{id}/mfa`, superadmin-only): clears another
user's factor for the lost-device case. This is a privilege-escalation path by
construction — it must be superadmin-gated (same as role assignment,
`apps/myidsan/apis/user_login.go:33-53`) and must be one of the first things written to
the audit log when that lands.

## 5. Which login paths get gated

| Path | Gated? | Why |
|---|---|---|
| Local password (`/api/login/default`) | **Yes** | myidsan IS the IdP here |
| LDAP/AD (`/api/login/ldap`) | **Yes, configurable** | myidsan replays the password against AD; AD applies no second factor in this flow |
| Kerberos SPNEGO | No | ticket already proves a domain-authenticated session; the DC owns factor policy |
| OIDC / Google / GitHub | No | the IdP owns MFA; double-prompting trains users to click through |

So the gate belongs at the two password call sites, **not** inside
`issueSessionCookies` — the Kerberos and OAuth callers deliberately bypass it. Put the
check in `issueLocalSession` and the LDAP handler, and add a short comment at
`issueSessionCookies` recording *why* it is not the chokepoint for this one concern.

## 6. Policy

New `RuntimeSetting` keys (runtime-editable, no restart):

- `mfa.policy` = `off` | `optional` | `required` (default `optional`)
- `mfa.requiredRoleIds` = list — "required for admins, optional for viewers"
- `mfa.applyToDirectory` = bool (the LDAP row in §5)

Under `required`, a user with no confirmed factor gets a session but is pinned to the
enrollment screen — that IS the must-change pattern, and it is safe here because the
password already succeeded and the user is being forced to *add* a factor, not to prove
one they already hold.

## 7. The lockout hazard (call this out in the runbook)

If the only superadmin enrolls MFA and loses the device **and** the recovery codes, the
entire suite is unadministrable. Mitigations, all three:

1. Recovery codes are mandatory at enrollment (not skippable).
2. A documented offline escape: a `bootstrap`-style config flag (e.g.
   `bootstrap.allowMfaReset=true` + restart) that clears MFA for the seeded superadmin,
   mirroring how `bootstrap.allowReset` gates factory reset ([[secure-wipe-reset-plan]]).
3. The first-run wizard should encourage a SECOND superadmin account before MFA is
   made `required`.

## 8. Phasing

- **P0** — `infra/mfa` (TOTP + RFC 6238 test vectors + skew/replay unit tests), the two
  entities, at-rest sealing of the secret. No HTTP surface.
- **P1** — enrollment/verify/recovery APIs + SPA screens (Account → Security), self-serve
  only, nothing gated yet. Ship-able and safe on its own.
- **P2** — the pre-session `mfaToken` exchange + gating the two password paths + both
  login surfaces (SPA and the server-rendered `/api/auth/login` page — remember there are
  two, per [[federated-login-social-buttons]]).
- **P3** — policy settings, admin reset, the bootstrap escape hatch, metrics
  (`mfa_challenge_total{result}` — a spike in failures is the signal that matters,
  per [[tier3-metrics]]).
- **P4** — WebAuthn/passkeys as a second `Kind`, reusing the whole §2 exchange.

## 9. Downstream impact: none

Because the second factor completes **before** any session cookie exists, the federation
contract (`/api/auth/authorize` → code → `/api/auth/token`) and every relying app are
untouched — the same property the enterprise-SSO phases preserved. No mymatasan,
myseliasan or myiotsan changes in any phase.

## 10. Verification

Unit: RFC 6238 published test vectors; skew boundary (±1 accepted, ±2 rejected); replay
rejection; recovery-code single-use; `mfaToken` rebinding rejection (changed IP/UA).

Live bench: extend the harness from [[myidsan-sso-bench-recipe]] — the throwaway instance
plus a scripted TOTP generator standing in for the authenticator app, asserting that
(a) password-only returns `mfaRequired` and sets **no** cookies, (b) a valid code
completes the session, (c) a replayed code fails, (d) LDAP logins gate when
`mfa.applyToDirectory` is on and Kerberos/OIDC never gate.
