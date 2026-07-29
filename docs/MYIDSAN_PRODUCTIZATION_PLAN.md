# MyIDSan — Productization Plan

Status: **Phase 0 DONE (LDAP, OIDC and Kerberos benches all complete); Phases 1, 2 and 3
DONE; Phase 4 §4.7 (audit log retention) DONE** (written 2026-07-29, against myidsan
v1.25.0; Phases 0 and 1 on `feat/myidsan-phase0-hygiene`, Phase 2 on
`feat/myidsan-phase2-audit-sessions`, Phase 3 on `feat/myidsan-phase3-policy`, §4.7 (and
the Kerberos bench) on `feat/myidsan-phase4-operability`, each stacked on the last). The
Generic OIDC bench and the Kerberos bench are both now done, against a real Keycloak 26
realm and a real Samba AD DC respectively — see `docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md`.
Both benches found the same class of gap: a rejected federated/Kerberos sign-in was
recorded only as a metric, never in the audit trail (§4.7 below).

Phase 1 outcome: `.idbackup` export/restore shipped with a superadmin-only API, an admin
page, and a "restore from a backup instead" path on the first-run wizard. Verified
end-to-end against a live instance: enrol MFA → export → destroy the database **and** the
at-rest key → fresh install → restore → sign in with the pre-disaster password and redeem a
TOTP code generated from the pre-disaster secret. The design point that makes that work is
that sealed columns are unsealed on export and re-sealed with the destination's key on
restore (§1.1 below), so the at-rest key never travels and the archive is never inert.

Phase 2 outcome — all five sub-phases done:

| Item | State |
|---|---|
| 2.1 Security audit log | **DONE** — append-only entity/service, no update path and no way to delete a chosen row; login success/failure/lockout/logout, user create/update/delete, MFA admin reset, directory change, password-reset resolution, backup export/restore, session revocation, step-up success/failure. The one exception, added in Phase 4 (§4.7): config-file-only, age-based, archive-first retention |
| 2.2 Audit UI and export | **DONE** — superadmin `GET /api/audit` + `/api/audit/export.csv` with a filter row, pagination and CSV export. `csvSafe()` defuses spreadsheet formula injection, because the export carries attacker-controlled text |
| 2.3 Write `user_session` | **DONE** — the table declared years ago and never written to is now populated, with `IpAddress`/`UserAgent`/`LastSeenAt` added |
| 2.4 Session administration | **DONE** — self-service list/revoke/revoke-others on Profile (auth-only, NOT matrix-gated), superadmin per-user revoke on Users, and **disabling or deleting an account now ends its sessions** |
| 2.5 Step-up re-authentication | **DONE** — 5-minute cache-backed marker keyed by session id, gating backup export/restore, MFA admin reset and password-reset resolution; the SPA re-runs the original action after the prompt |

Phase 3 outcome — all five sub-phases done:

| Item | State |
|---|---|
| 3.1 Password policy | **DONE** — enforced at all FOUR paths (previously two, and admin account creation had no check at all). Default minimum 12; character-class rules default off on purpose; embedded common-password denylist on by default. Server-generated credentials exempt, with a test proving they still satisfy the default policy |
| 3.2 MFA enforcement | **DONE** — `mfa.policy` off/optional/**required** (default optional), narrowable by role, directory accounts out of scope unless opted in. Enforced AFTER the password succeeds: the user gets a session but is pinned to enrolment, so switching the policy on does not lock out every existing admin |
| 3.3 Client secrets | **DONE** — bcrypt replaces unsalted single-round SHA-256. Legacy hashes still authenticate and are rewritten the first time their secret is presented, so installs migrate themselves |
| 3.4 Per-account lockout | **DONE** — the guard was IP-only, so a spray distributed across addresses against one account was unthrottled. Lock check moved after request decoding so the username is available |
| 3.5 CSRF on auth forms | **DONE** — session-less double-submit token on the four public server-rendered forms, which previously had none at all |

Two gotchas from Phase 3 worth remembering. The CSRF token must be minted **before**
`WriteHeader`: `http.SetCookie` appends to the header map, so a cookie set from inside the
`Fprintf` argument list is silently discarded, and every genuine submission then fails
while forged ones still look correctly rejected. And the per-account lockout is a
deliberate tradeoff — an attacker who knows a username can lock that user out, which is a
nuisance they recover from by waiting, versus unlimited guessing which they do not.

Two things worth carrying forward. First, `middlewares.ClientIP` was extracted from the
rate limiter so the audit trail shares one trusted-proxy implementation — myseliasan's
audit log has its own copy that trusts `X-Forwarded-For` unconditionally, and an audit
trail whose source address the caller can choose is worse than one with no address at all.
Second, the self-service session routes are deliberately **not** behind the RBAC matrix: the
first implementation put them there and an ordinary user got a 403 listing their own
sessions, which is exactly backwards — the person whose laptop was stolen is the one who
needs that screen.

Phase 0 outcome, in brief — full detail in each sub-phase below:

| Item | State |
|---|---|
| 0.1 LDAP live bench | **DONE** — 9 integration tests green against OpenLDAP over LDAPS *and* StartTLS (`infra/login/ldap_integration_test.go`) |
| 0.1 OIDC live bench | **DONE (2026-07-29)** — against a real Keycloak 26 realm: discovery, PKCE S256 + nonce + state, `id_token`/JWKS verification, session issuance, and refusal of a tampered state/missing flow cookies/unverified email. Found and fixed one gap: refused federated sign-ins were not audited (see §4.7) |
| 0.1 Kerberos bench | **DONE (2026-07-29)** — against a real Samba AD DC (realm `KOPI.TEST`): `kinit`-obtained ticket signs in, no-token request gets the `Negotiate` challenge, forged token is refused with no session (verdict read from `Location` + the `kopiv2_access` cookie, since both outcomes answer `302`). Found and fixed the same gap as the OIDC bench: a rejected ticket was recorded only as a metric, never audited (see §4.7). Realm allow-list exercised only in the accepting direction — a cross-realm rejection remains unbenched |
| 0.2 Dead PKCE/refresh controls | **DONE** — removed from form, DTO and admin API; columns kept and annotated |
| 0.3 Config + git hygiene | **DONE** — lockout now defaults ON in code, 8 private keys untracked, secrets scrubbed, `sso.internalToken` placeholder guard |
| 0.4 CI gate | **DONE** — `.github/workflows/go-check.yml`; found and fixed a stale test that had been failing unnoticed |
| 0.5 Swagger air-gap | **DONE** — vendored swagger-ui-dist@5.32.11, no inline script, live-verified |
| 0.6 Correctness fixes | **DONE** — toast kinds, bulk-delete confirm, backslash open redirect, account-existence oracle |

Goal: take myidsan from "the SSO hub the kopiv2 suite happens to include" to something a
customer's IT department will accept, pay for, and operate without you in the room.

This plan is the sequenced form of the sell-readiness audit. The audit's finding is that
the *engineering* is ahead of the *product*: authentication depth (LDAP/AD, Kerberos
SPNEGO, generic OIDC, TOTP with a pre-session challenge), packaging, and first-run are
genuinely strong, while the gaps are the boring checkable things a buyer asks for first —
backup, audit, session control, and standards conformance.

Two audit findings were checked against source and **withdrawn** — do not re-plan them:

- *"Login lockout is off by default."* True only for `apps/myidsan/config.json`, which
  developers run via `go run . -app myidsan`. Every shipping channel installs
  `deploy/dist/myidsan-config.json`, which enables lockout, ships a strict CSP, and leaves
  all secrets blank for first-run generation. Phase 0.3 reconciles the two files; there is
  no customer-facing exposure.
- *"Relying apps share myidsan's HMAC signing secret."* They do not. An RP calls the token
  endpoint over TLS on the back channel, reads identity out of the JSON body
  (`apps/myseliasan/apis/auth.go:334-341`), and mints its own session with its own
  independently generated `jwt.secret`. The trust boundary is sound.

---

## The fork in the road — DECIDED: selling the suite

**Decision (2026-07-29): the product is the kopiv2 suite, not myidsan standalone.**
**Phase 5 (OIDC conformance) is therefore SKIPPED.**

The question was:

> Are you selling **the kopiv2 suite** (with myidsan as its identity layer), or selling
> **myidsan as a standalone identity product**?

The answer is the suite. myidsan only ever talks to mymatasan, myseliasan and myiotsan,
which are all clients written against its own authorization-code flow, so the bespoke flow
is adequate and Phase 5 is optional polish rather than a blocker.

What that decision buys and what it costs, stated plainly so it can be revisited on
evidence rather than re-argued from memory:

- **Buys:** the largest phase in this plan disappears. No asymmetric signing and JWKS with
  key rotation, no discovery document, no `id_token`/userinfo/scope model, no consent
  screen, no refresh-token grant, no back-channel logout, no conformance suite. Market date
  moves in by a large margin.
- **Costs:** no third-party software can be put behind myidsan. Grafana, GitLab, Nextcloud,
  Proxmox and a customer's own OIDC-ready application cannot integrate, and will not be able
  to without doing Phase 5. Marketing must therefore not describe myidsan as a general
  "SSO hub" — the honest framing is "single sign-on across the kopiv2 suite".

**Revisit this if** a prospect asks to put their own application behind it, or if myidsan
is ever priced or sold separately from the suite. Nothing built in Phases 0–4, 6 or 7
forecloses doing Phase 5 later; it simply is not on the path to this product.

Remaining path: **Phase 4 (operability) → Phase 6 (end-user product) → Phase 7 (commercial)**.

---

## Non-goals

- **SAML.** Stays parked, per [MYIDSAN_ENTERPRISE_SSO_PLAN.md](./MYIDSAN_ENTERPRISE_SSO_PLAN.md).
  Revisit on concrete demand only; the XML-dsig attack surface is not worth speculative work.
- **Multi-tenancy.** One deployment per customer. This is a defensible on-prem product
  decision, but it rules out SaaS and MSP hosting and is currently undocumented — Phase 7
  writes it down as a stated boundary rather than leaving buyers to discover it.
- **Phone-home telemetry.** Contradicts the air-gap positioning, which is one of the
  product's few real moats. Phase 7's licensing is an offline signed file for the same reason.
- **SCIM provisioning.** Deferred on the SAML pattern — real, but wait for a customer to ask.
  Note the consequence in the meantime: a user deleted in AD keeps their myidsan account and
  role, they merely stop being able to authenticate.
- **PAM, RADIUS, trusted-header proxy auth.** Unchanged from the enterprise SSO plan.

---

## Phase map

| Phase | Name | Gate it clears | Blocking? |
|---|---|---|---|
| 0 | Truth &amp; hygiene | Before you demo to anyone who might pay | Yes |
| 1 | Survivability | Before first install you don't control | Yes |
| 2 | Accountability | Before a security review | Yes |
| 3 | Policy | Before a security review | Yes |
| 4 | Operability | Before first paid deployment | Yes |
| 5 | OIDC conformance | ~~Only if selling standalone~~ | **SKIPPED** — suite chosen |
| 6 | End-user product | Before charging per-seat | Recommended |
| 7 | Commercial | Before invoicing | Yes |

Sizing below is relative (S / M / L) and distinguishes **port** (the pattern exists in a
sibling app and is being adapted) from **net-new**. Ports are dramatically cheaper and are
deliberately front-loaded.

---

## Phase 0 — Truth &amp; hygiene

**Everything here makes an existing claim true, or deletes the claim.** Nothing is a new
feature. This is the cheapest phase and the one that most changes how the product reads.

### 0.1 Live-bench LDAP, Kerberos, and OIDC — *net-new, M* ← **do this first**

`apps/myidsan/README.md:11-13` says of each of the three federation paths, in the shipped
documentation, *"Not yet live-tested against a real directory / KDC / IdP."* These are the
enterprise features being sold. Until this runs they are unverified claims.

The harness already exists: [[myidsan-sso-bench-recipe]] — throwaway Samba AD DC plus
Keycloak, with LDAP StartTLS and LDAPS both previously verified. Known gotchas recorded
there: `samba-dsdb-modules`, a named volume for xattrs, the cert needs an IP SAN, and
bench the real SUT rather than `ldapsearch`.

Assert per path: bind succeeds; group → role mapping resolves; `Authoritative` on re-applies
the mapping every login and off seeds pending accounts only; a directory user and an LDAP
password login for the same person land on **one** account; Kerberos principal resolution
reaches that same account; a bad keytab degrades to "not offered" rather than failing boot.

This is first because it can invalidate later phases. Everything downstream assumes these
work.

**DONE (2026-07-29) — and it did invalidate an assumption.** All three paths are now
benched against real infrastructure and the README claims replaced with results:

| Path | Infrastructure | Outcome |
|---|---|---|
| LDAP | Samba AD DC, StartTLS + LDAPS | passed (earlier run) |
| OIDC | Keycloak 26 realm | passed: discovery, PKCE S256, nonce, state, `id_token` verified against real JWKS, session issued and accepted by a later API call |
| Kerberos | Samba AD DC, realm `KOPI.TEST` | passed: `kinit` ticket over SPNEGO signs in; no token gets the `Negotiate` challenge; forged token refused with no session |

Negative cases hold on both new paths: a tampered `state`, a callback with no flow
cookies, an IdP-reported `email_verified:false`, and a forged SPNEGO token are each
refused without issuing a session.

**The benches found two real defects that no unit test had caught**, both now fixed with
regression tests:

1. `providerCallback` audited only *successful* federated sign-ins. Every refused SSO
   attempt was invisible on the Audit log page, while a refused *password* login was
   recorded — so "show me the failed sign-in attempts" silently omitted all of SSO.
2. `kerberosLogin` had the same gap in a different shape: a rejected ticket only
   incremented a Prometheus counter. `recordFederatedLogin` is a **metrics** call, not an
   audit call — a distinction that reads as auditing at a glance and is why this survived.

Both fixes deliberately leave the failed-login lockout alone: the credential was checked
at the IdP or KDC, not guessed here, so counting these would let a rotated client secret,
a clock skew, or a stale keytab lock out every user at once. The SPNEGO *no-token* request
stays unaudited on purpose — it is the first half of every handshake, and recording it
would add an entry per request and bury the real rejections.

Remaining unbenched, and stated as such in the README rather than claimed: the Kerberos
realm allow-list was exercised only in the accepting direction (single-realm bench), and
the OIDC `groups_claim` → `FederatedGroupMapping` role seeding was not driven end-to-end
(the claim was emitted by a Keycloak group-membership mapper and the account correctly
landed with **no** role under pending-clearance, but no mapping row was configured).

### 0.2 Delete the dead controls — *S*

`RequirePKCE`, `AllowRefreshToken`, and `RefreshTokenTTLSeconds` are stored on
`AppAuthConfig`, round-tripped through the admin API, and rendered as live checkboxes and
an input on the app registration form. **No code path reads any of them** —
`code_challenge` is never parsed at authorize, and the token endpoint rejects
`grant_type=refresh_token`.

Hint text was added saying so, which is not enough: a checkbox that persists a value reads
as functional, and an operator who ticks "Require PKCE" will believe PKCE is enforced.

Remove all three from the form, the DTO, and the admin API. Leave the columns in place
(dropping them is a migration for no benefit) with a comment pointing at Phase 5.3/5.4,
which reintroduces both properly. See [[myidsan-app-registration-guide]].

### 0.3 Reconcile the two configs and clean the repo — *S*

`apps/myidsan/config.json` and `config.dev.json` are **tracked in git** and contain a real
`jwt.secret`, a Redis password, a DB password, `admin123`, a live Google `client_id`, and
`"internalToken": "change-me-in-production"`. `apps/myidsan/certs/key.pem` is tracked too.
`apps/*/secret/` is correctly gitignored; `certs/` is not.

Customers never receive these — they get `deploy/dist/myidsan-config.json` — but they are
public the moment the repo is, and the dev config is a bad example someone will copy.

- Add `loginSecurity` and `securityHeaders` to the dev config so it matches what ships.
  Better still, default `LoginSecurity.Enabled` to true when the block is absent
  (`apps/myidsan/app/app.go:384` currently passes `ls.Enabled` straight through with no
  default, and `domain/shared/apis/login_guard.go:89` makes a disabled guard a silent no-op).
- Gitignore `apps/*/certs/` and both config files; ship `config.example.json`.
- Rotate everything that leaked.
- Add a weak-value guard for `sso.internalToken` mirroring the one `jwt.secret` already has
  in `infra/apphost/run.go:839-966`, and make its comparison constant-time
  (`apps/myidsan/apis/sso.go:81-85` uses `==`).

### 0.4 Turn on CI — *S*

**No workflow in `.github/workflows/` runs `go test`, `go vet`, or a linter.** The only Go
command in CI is `go build`, inside the four release workflows. myidsan has 10 test files
and the shared modules have more; none of them run automatically.

Add a PR gate: `go build ./...`, `go vet ./...`, `go test ./...`. Add `govulncheck` while
you are there — Dependabot currently covers the npm frontends only, and the auth path
carries `gokrb5 v8.4.4` (last released 2022) and `skip2/go-qrcode` (2020).

### 0.5 Self-host Swagger UI — *S*

`infra/apidocs/openapi.go:1141,1149` load Swagger UI's CSS and JS from
`cdn.jsdelivr.net`. This breaks on any air-gapped install **and** violates the strict CSP
myidsan itself ships (`script-src 'self'`), so `/swagger` renders blank in both cases.
`/swagger/openapi.json` is unaffected.

Vendor the `swagger-ui-dist` assets and serve them from the app. This is suite-wide — the
fix lands in shared `infra/apidocs` and benefits all four apps. Air-gap operation is one of
your genuine differentiators; shipping a CDN reference undercuts the pitch.

### 0.6 Correctness fixes worth doing while you're here — *S*

| Fix | Where |
|---|---|
| Toast `kind: 'danger'` matches no CSS class (only `success`/`error` exist), so 8 error paths render as neutral grey | `App.js` Profile / Reset requests / avatar; `frontend/shared/src/styles/toast.css:38-39` |
| Bulk delete fires with no confirmation on Groups, Roles, Endpoints — while single-app delete *does* prompt | `CrudPage.removeSelected` |
| `cleanContinuePath` rejects `//evil.com` but accepts `/\evil.com`, which browsers normalise into a protocol-relative redirect | `apis/federated_auth.go:933-945` |
| Login returns "account is inactive" and "managed by third-party login" pre-authentication — a positive account-existence oracle | `services/user_login.go:98-104` |
| `INITIAL_ADMIN_LOGIN.txt` is left in the app dir after the password is changed | `app/firstrun.go` |

**Phase 0 exit:** the three federation paths are verified working, nothing in the UI claims
a capability that doesn't exist, CI runs tests, and `/swagger` works offline.

---

## Phase 1 — Survivability

### 1.1 Backup and restore — *port from mymatasan, M*

There is no backup API, no scheduled dump, no restore path, and no documented DR procedure.
`deploy/README-myidsan.md:109-117` tells the operator to copy the database and
`secret/atrest.key` themselves, as a pair.

That database holds every user, every role, every registered SSO client, the SSO CA private
key, all TOTP secrets, and the LDAP bind password. Losing it locks every employee out of
every app in the suite at once. This is the first question an IT buyer asks.

The pattern is already built and tested in `apps/mymatasan/apis/backup.go`,
`apps/mymatasan/services/backup.go`, and `backup_test.go` — a portable passphrase-encrypted
archive with FK remapping on restore and secrets carried via shadow fields, per
[[backup-restore-plan]]. Port it.

myidsan-specific care:

- **The at-rest key must travel with the backup or the backup is inert.** TOTP secrets and
  the LDAP bind password are sealed by `infra/atrest`; a database-only restore yields
  accounts whose second factor cannot be verified. Either seal the key into the archive
  under the backup passphrase, or refuse to produce a backup without an explicit
  acknowledgement — silently producing an unrestorable archive is the worst option.
- The SSO CA private key lives in the DB, so a restore re-establishes issued client certs.
- Restoring onto a live instance must invalidate cached sessions, or restored users inherit
  pre-restore sessions.

### 1.2 First-run restore — *S*

Offer "restore from backup" in the setup wizard, mirroring mymatasan. A customer whose host
died needs this path to exist before they have a working superadmin.

### 1.3 Document the DR procedure — *S*

Backup cadence, what the archive contains, how to verify one restores, and the RTO a
customer should expect. A backup feature nobody trusts is not a backup feature.

**Phase 1 exit:** a customer can lose the host and come back.

---

## Phase 2 — Accountability

Audit and session control are one phase because they answer one question — *who did what,
and can I stop them* — and a reviewer will ask both together.

### 2.1 Security audit log — *port from myseliasan, M*

myidsan has no audit entity, service, or API. myseliasan has all three
(`apps/myseliasan/entities/audit_log.go`, `services/audit.go`, `apis/audit.go`). Today
myidsan's security-relevant events are free-text `log.Printf` lines in a rolling file, with
no structure, no UI, no export, and cleanup disabled by default in the shipped config.

Events that must be recorded: login success and failure (with method — local / LDAP /
Kerberos / OIDC / social), lockout engaged, MFA enrol / disable / admin reset, recovery-code
consumption, role assignment and role change, user create / disable / delete, app
registration and client-secret rotation, redirect-URI change, password-reset request and
resolution, directory config change, session revocation.

Each row: timestamp, actor user ID and email, target, action, source IP, user agent, outcome.

### 2.2 Audit UI and export — *S*

An **Audit** page under Administration with filtering by actor, action, target, and date
range, plus CSV export. `/api/log` and `/api/log-service` exist but are seeded without menu
metadata, so nothing is reachable from the console today. Compliance reporting is the whole
point; an audit log with no export does not serve it.

### 2.3 Start writing `user_session` — *S*

`domain/entities/user_session.go` is declared, registered at `apps/myidsan/app/app.go:56`,
and its table is created — and **nothing ever writes or reads a row**. The comment says
"for audit and future revocation flows"; this is that future.

Write a row on session issue, mark it revoked on logout and on expiry. The entity needs
three fields added for the screen in 2.4 to be useful: `IpAddress`, `UserAgent`,
`LastSeenAt`. The bootstrap auto-adds missing columns from the struct, so this is additive.

> **Infra gotcha:** listing sessions for a user is a foreign-key query, and
> `dbsql.GetByForeign` returns only **one** child row (hardcoded `limit=1`) —
> see [[getbyforeign-limit1-bug]]. Use `Get` with an `Equal` filter on the FK instead.

### 2.4 Session administration and self-service — *net-new, M*

- Admin: list a user's active sessions, revoke one, revoke all.
- User: "your active sessions" on the Profile page with device, IP, last-seen, and a
  sign-out control — plus "sign out everywhere".
- **Disabling or deleting an account must terminate its live sessions.** Today
  `validateSession` checks the cache entry, not `IsActive`, so a disabled user's session
  survives; RBAC-gated routes block, but auth-only routes (`/api/profile/*`, `/api/mfa`,
  change-password) stay reachable.

### 2.5 Admin step-up re-authentication — *S*

`RequireSuperadmin` gates role changes, MFA admin-reset, and password-reset resolution on
role alone. A stolen 72-hour session cookie is therefore full superadmin. Require a fresh
password or TOTP confirmation for those actions, on a short re-auth window.

**Phase 2 exit:** you can answer "who granted that role, from where, and when" and "sign
that person out of everything" — the two questions that end security reviews.

---

## Phase 3 — Policy

### 3.1 Password policy — *net-new, M*

The only rule anywhere is `len(trimmed) >= 8`, applied in exactly two of the four
password-setting paths (`ChangePassword` and `SetPasswordSelfService`). **Admin-created
accounts and self-registration have no minimum at all.** There is no complexity check, no
expiry, no history, no reuse prevention, and no breach check. The single sanity rule is
"username ≠ password".

Add a `passwordPolicy` config block — minimum length, character classes, optional
`LastLoginAt`-based dormancy — and apply it at **all four** call sites behind one
validator. A local denylist of the common-password top-N is worth more than an HIBP
integration here, because HIBP needs egress that the air-gap positioning forbids.

### 3.2 MFA enforcement policy — *net-new, S*

MFA is entirely opt-in per user, with no org or role-level requirement, no enrolment grace
period, and no admin view of who has enrolled. [[myidsan-mfa-plan]] documents `mfa.policy`
and `mfa.requiredRoleIds` as designed but not built — build them, plus a "MFA enrolled"
column on the Users page. At minimum: superadmins must enrol.

Note that Kerberos and OIDC logins deliberately bypass the local factor (the upstream IdP
owns factor policy); the policy layer must not accidentally lock those users out.

### 3.3 Rehash client secrets — *S*

`hashClientSecret` is an **unsalted single-round SHA-256**
(`apis/federated_auth.go:956`). A database read gives offline recovery of operator-chosen
secrets at GPU speed. Move to bcrypt, or HMAC-SHA256 under a server key. Support both on
read during a transition, and rehash on next successful client authentication.

### 3.4 Per-account lockout — *S*

`loginGuardKey` returns only `"ip:"+host`, so distributed password spray against one
account is unthrottled. The guard already accepts multiple keys — pass an account key
alongside the IP key. Also note the guard is in-process memory, so lockouts evaporate on
restart and do not coordinate across instances; move its state to the cache, which is
already a hard boot dependency.

### 3.5 CSRF on the server-rendered auth forms — *S*

`POST /api/auth/login`, `/api/auth/mfa`, `/api/auth/forgot`, and `/api/auth/reset` are
public routes that never pass through the auth middleware, and the rendered forms carry no
token. Authenticated JSON APIs are correctly protected by the double-submit cookie; these
four are not. Enables login-CSRF and forced reset submission.

**Phase 3 exit:** the product enforces the policies a reviewer assumes exist.

---

## Phase 4 — Operability

### 4.1 Settings UI — *port, M*

myidsan is the **only** app in the suite without one: `apis/settings.go` exists in
myseliasan, mymatasan, and myiotsan. SMTP, lockout thresholds, session TTL, Kerberos, and
OIDC providers are config-file-only and need a restart; adding an OIDC identity provider
means editing JSON on the server. Only LDAP has a UI.

Port myseliasan's pattern, including its `settings_apply.go` / `settings_materialize.go`
edits-to-`config.json`-plus-restart seam — see [[myseliasan-settings-feature]]. Keep the
same safe-subset exclusions (db, server, bootstrap). Add a "send test email" action for
SMTP; there is no way to verify mail configuration today.

### 4.2 The HA boundary — *S, mostly documentation* — **DONE**

Sessions are cache-backed, and the shipped config uses the in-process memory cache — so
**two instances behind a load balancer silently log users out**. Redis is supported and is
the answer, but nothing in `deploy/README-myidsan.md` tells a myidsan operator that the
default is single-instance-only.

Document it, and make the app log a startup warning when it detects a memory cache in what
looks like a multi-instance deployment. The LoginGuard state move in 3.4 is part of the
same story.

**DONE.** Every boot now states the boundary (`warnSharedStateBoundary`): a shared cache
confirms the app can sit behind a load balancer; a per-process cache says plainly that the
instance is SINGLE-INSTANCE ONLY, what breaks if it is not (users signed out on every
switch, all sessions lost on restart), and how to fix it.

The *loud* case is deliberately a contradiction rather than a guess. A process cannot tell
from the inside whether it is one of several replicas, and a warning that fires on healthy
single-instance installs is one operators learn to ignore — which costs the real one. But an
operator who configured a **distributed transaction lock** has already said they expect more
than one instance, since that is the only reason to pay for one. A distributed lock beside a
per-process session cache is therefore a self-inconsistent configuration, and saying so is a
fact about the config rather than a hunch about the topology.

### 4.3 Resource limits — *S* — **connection pool DONE; log size cap OUTSTANDING**

- ~~`SetMaxOpenConns` is set only for SQLite. Postgres and MariaDB use Go's default —
  unlimited — and will exhaust a customer's connection budget under load.~~ **DONE.**
  `db.pool` (`maxOpenConns` / `maxIdleConns` / `maxLifetimeSeconds` / `maxIdleTimeSeconds`,
  plus an explicit `unlimited` escape hatch) is applied by both server engines. The
  defaults matter more than the knobs: an absent block now yields a *bounded* pool (25/5,
  30-minute lifetime), never Go's unlimited default, so forgetting to configure it can no
  longer take down every other application sharing that database server.

- **The second bullet was wrong and is corrected here.** It claimed the existing cleanup
  "prunes database rows, not the `.log` files on disk". It does prune the files:
  `startRuntimeLogCleanupScheduler` → `runtimeLogService.DeleteOlderThan` →
  `fileLogger.DeleteOlderThan`, which removes dated `.log` files, and
  `deploy/dist/myidsan-config.json` ships with `logging.cleanup.enabled: true` at 90 days.
  (The in-repo dev config has it off, which is what made this look absent.)

  What is genuinely missing is a **size cap**: rotation is purely by calendar day, so a
  single chatty or hostile day can fill the disk long before the 90-day retention is
  relevant. Deliberately not implemented in a hurry, because it has a trap — retention
  finds files via `logFiles()` and `dateValueFromPath()`, so a size-based rotation that
  introduces a new filename shape (`…-2026-07-29.1.log`) without teaching those two
  functions to recognise it would leave the extra files **undeletable forever**, causing
  exactly the disk exhaustion the cap was added to prevent. Any implementation must change
  the parser and the writer together, with a test that a sequenced file is still pruned.

### 4.4 IdP metrics — *S* — **DONE**

Two app-specific metrics today (`myidsan_federated_login_total`,
`myidsan_mfa_challenge_total`), and myidsan is absent from the metrics catalogue in
`docs/HOWTO.md:317-378` while myiotsan and myseliasan are documented. Per [[tier3-metrics]],
instrument what fails silently: active sessions, token issuance and exchange failures,
authorization-code redemption failures, upstream LDAP/OIDC latency and error rate, SSO CA
expiry as a gauge. Then add the catalogue section.

**DONE.** Four metrics added and myidsan is now in the `docs/HOWTO.md` catalogue alongside
the other three apps. The selection follows [[tier3-metrics]] — instrument what fails
*silently* — which for an IdP means the failures somebody ELSE experiences:

| Metric | Why it exists |
|---|---|
| `myidsan_token_exchange_total{outcome}` | Every non-success is a **relying app that just failed to sign a user in**. The app shows its own user its own error; nothing here raises anything an operator would notice. Nine closed-set outcomes, so `secret_invalid` (a rotated secret) is alertable separately from the ordinary `code_invalid` race. |
| `myidsan_audit_write_failures_total` | The audit service swallows write errors **by design** — auditing must never fail the action being audited — so a trail that has stopped recording has no other symptom. Alert on any increase. |
| `myidsan_audit_retention_purged_total` | Makes trail shrinkage attributable: rows vanishing without this moving did not come from retention. |
| `myidsan_sessions_active` | Capacity, and confirming a revocation actually took effect. |

Verified live, not just unit-tested: driving two different token failures against a running
instance produced `myidsan_token_exchange_total{outcome="client_unknown"} 1` and
`{outcome="unsupported_grant"} 1` on a real `/metrics` scrape, with the help text attached.

Two things worth recording about the session gauge. It is **polled** rather than incremented
at the call sites, because sessions also end without anyone calling `Revoke` (a cache entry
simply expires), so a hand-maintained counter would drift from the truth and never recover.
And `CountActive` filters on a boolean column while the query builder renders bool filter
values as quoted literals — if the engine stored them as `0`/`1` the filter would match
nothing and the gauge would read a permanent zero that looks exactly like "no sessions". A
fake repo cannot answer that (it compares Go values, so it agrees with whatever was assumed),
so it is verified against a real sqlite database instead.

**Not done from this list:** upstream LDAP/OIDC latency and error rate, and SSO CA expiry as
a gauge. Both are worth having — a CA that expires takes the whole fleet down at once with
no warning, which is the definition of a silent failure — but they need instrumentation
inside the directory client and the CA service rather than at a handler boundary.

### 4.5 ZAP scan and k6 load profile — *port, M*

Both tools already have per-app plans for the other three apps and **none for myidsan** —
no `myidsan-*.yaml` in `tools/zaproxy/plans/`, no `myidsan-*.js` in `tools/k6/scripts/`.
The identity provider is simultaneously the highest-value attack surface in the suite and
the component every request depends on, and it is the only one that has never been scanned
or load-tested.

Known gotchas from [[myseliasan-zap-hardening]] and [[k6-load-testing-tool]]: disable the
rate limiter during a scan, the full ZAP plan OOMs Docker, swagger paths need
`{id:[0-9]+}`, k6 resets the cookie jar per iteration, and a must-change-password state
blocks reads.

### 4.6 Reverse-proxy guidance — *S* — **DONE (docs); one code fix deferred, see below**

One sentence exists today. Nearly every real deployment terminates TLS upstream. Ship
sample nginx and Caddy configs, document the `X-Forwarded-*` trust model, and — critically
for an IdP — explain that the public issuer URL must match what redirect URIs are
registered against.

**DONE.** `deploy/reverse-proxy/` now holds working `nginx-myidsan.conf`, a
`Caddyfile.myidsan`, and a README covering: forwarding the *client's* hostname (`$host`,
never `$proxy_host`) because registered redirect URIs are matched exactly and a drifted
public URL fails every relying app at the **callback**, after the user has already signed
in; overwriting rather than appending `X-Forwarded-*`; setting `rateLimit.trustedProxies`
so the audit trail and the lockout key on the real client instead of counting the whole
building as one address; disabling upstream keep-alive when Kerberos is on, since SPNEGO
authenticates a **connection** rather than a request; and an air-gap note, because Caddy's
automatic HTTPS needs an ACME CA it cannot reach.

**The flagged inconsistency is real — verified, not assumed.** `IsSecureRequest` in
`domain/utils/middlewares/auth.go` honours `X-Forwarded-Proto: https` from any caller
despite a comment claiming it requires a trusted proxy, while `ClientIP` does consult the
allow-list.

Assessed before acting on it, so it is neither ignored nor over-read: the check is
`r.TLS != nil || XFP == https`, so the header can only make a request look *more* secure,
never less — a genuine HTTPS request cannot be downgraded. It selects the cookie name and
`Secure` flag, so a caller asserting it over cleartext makes the server set `Secure`
cookies their own browser then refuses to store. **That is a self-inflicted denial of
service, not privilege escalation or session fixation.**

Deferred rather than rushed: the fix threads the trusted-proxy allow-list through five
call sites across the shared middleware, myidsan and myseliasan, and it decides cookie
naming — getting it wrong breaks authentication suite-wide, which is far worse than the
availability issue it closes. Mitigated meanwhile by the sample configs, which strip the
header at the edge.

### 4.7 Audit log retention — **DONE**

The security trail (§2.1) was append-only with genuinely no delete path at all — the right
default, but an unbounded table on a long-lived install is still a real disk-growth problem
nobody had a lever for. `config.audit.retention` (off by default) adds the one sanctioned
exception, shaped so it cannot become a way to quietly erase an inconvenient event: it is
config-file-only (no API — trimming the trail needs filesystem access to the server, not a
session on it), it takes an age rather than a selection of rows, expired rows are archived to
a JSON-lines file and fsynced/renamed into place **before** anything is deleted from the
table (a run that cannot finish its archive deletes nothing), and the purge records itself
back into the trail (`audit.retention_purge`, naming the cutoff, row count, and archive file)
so a reader who finds history starting abruptly can tell a deliberate trim from an empty
past. `maxRetentionDays` defaults 365 with a hard 30-day floor (anything configured lower is
raised and a startup warning logged); `frequencyHours` defaults 24; `archiveDir` defaults
`audit-archive`, data-dir-relative, and its files are unsealed (0600) — treat that directory
with the same care as the database. See `infra/config/audit_retention.go.md`,
`apps/myidsan/services/audit_retention.go.md`, `apps/myidsan/app/audit_retention.go.md`, and
`docs/HOWTO.md`'s "MyIDSan Audit Log Retention" section.

Alongside this, live benches against a real Keycloak 26 realm and a real Samba AD DC found
and fixed the same class of audit gap in two different handlers: the federated (OIDC/social)
callback and Kerberos SPNEGO login had each audited only successful sign-ins, so every
refused SSO/ticket attempt was invisible on the audit page while a refused password login
was recorded. All four federated rejection paths now record `login.failure` naming the
provider (`recordFederatedLoginFailure`), and all three Kerberos rejection paths do the same
naming `services.MethodKerberos` (`recordKerberosLoginFailure`) — except the no-token
challenge, which is the first half of every SPNEGO handshake and deliberately stays
unaudited. Neither path advances the failed-login lockout — the credential was checked at
the IdP or against the keytab, not guessed against myidsan. See `apps/myidsan/apis/login.go.md`
and `docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md` for both bench statuses.

### 4.7 Audit log retention — **DONE**

The security trail (§2.1) was append-only with genuinely no delete path at all — the right
default, but an unbounded table on a long-lived install is still a real disk-growth problem
nobody had a lever for. `config.audit.retention` (off by default) adds the one sanctioned
exception, shaped so it cannot become a way to quietly erase an inconvenient event: it is
config-file-only (no API — trimming the trail needs filesystem access to the server, not a
session on it), it takes an age rather than a selection of rows, expired rows are archived to
a JSON-lines file and fsynced/renamed into place **before** anything is deleted from the
table (a run that cannot finish its archive deletes nothing), and the purge records itself
back into the trail (`audit.retention_purge`, naming the cutoff, row count, and archive file)
so a reader who finds history starting abruptly can tell a deliberate trim from an empty
past. `maxRetentionDays` defaults 365 with a hard 30-day floor (anything configured lower is
raised and a startup warning logged); `frequencyHours` defaults 24; `archiveDir` defaults
`audit-archive`, data-dir-relative, and its files are unsealed (0600) — treat that directory
with the same care as the database. See `infra/config/audit_retention.go.md`,
`apps/myidsan/services/audit_retention.go.md`, `apps/myidsan/app/audit_retention.go.md`, and
`docs/HOWTO.md`'s "MyIDSan Audit Log Retention" section.

Alongside this, live benches against a real Keycloak 26 realm and a real Samba AD DC found
and fixed the same class of audit gap in two different handlers: the federated (OIDC/social)
callback and Kerberos SPNEGO login had each audited only successful sign-ins, so every
refused SSO/ticket attempt was invisible on the audit page while a refused password login
was recorded. All four federated rejection paths now record `login.failure` naming the
provider (`recordFederatedLoginFailure`), and all three Kerberos rejection paths do the same
naming `services.MethodKerberos` (`recordKerberosLoginFailure`) — except the no-token
challenge, which is the first half of every SPNEGO handshake and deliberately stays
unaudited. Neither path advances the failed-login lockout — the credential was checked at
the IdP or against the keytab, not guessed against myidsan. See `apps/myidsan/apis/login.go.md`
and `docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md` for both bench statuses.

**Phase 4 exit:** a competent sysadmin can deploy, monitor, and tune it without you.

---

## Phase 5 — OIDC conformance — **SKIPPED**

> **Not being built.** The product is the kopiv2 suite, so nothing outside it needs to
> integrate. Kept below in full because the decision is reversible: if a prospect ever asks
> to put their own application behind myidsan, this is the work, and the sub-phase ordering
> (5.1 gates everything after it) still holds.

The largest phase. It converts myidsan from a bespoke SSO for three known clients into
something any OIDC-capable software can sit behind. Sub-phases are ordered by dependency;
5.1 gates everything after it.

### 5.1 Asymmetric signing, JWKS, and key rotation — *net-new, L* ← **prerequisite**

Today: HS256, one symmetric secret, no `kid`, no rotation path — rotating invalidates every
session and every relying app simultaneously.

Move to RS256 or EdDSA with a keypair generated at first boot and sealed with
`infra/atrest`. Publish `/.well-known/jwks.json`. Add `kid` to the header and a
verification key set so rotation overlaps rather than cuts over. Keep HS256 verification
during a transition window so existing suite RPs keep working.

### 5.2 Discovery, `id_token`, userinfo, scopes and claims — *net-new, L*

`/.well-known/openid-configuration`; a real `id_token` alongside the access token; a
`userinfo` endpoint; a scope model (`openid profile email groups`) with per-client allowed
scopes; `nonce` handling on the IdP leg (it is already correct on the RP leg). Move the
token response to the RFC 6749 shape (`access_token`, `token_type`, `expires_in`) —
keep the current camelCase body behind a per-client compatibility flag so the three suite
apps do not break on upgrade.

### 5.3 PKCE and public clients — *net-new, M*

Parse and enforce `code_challenge` / `code_verifier` (S256), reintroducing the
`RequirePKCE` flag deleted in Phase 0.2 — this time wired. Add a public/confidential client
type, since today every client must present a secret, which means SPAs and mobile apps
cannot integrate at all.

### 5.4 Refresh tokens — *net-new, M*

Implement the `refresh_token` grant with rotation and reuse detection, reintroducing
`AllowRefreshToken` and `RefreshTokenTTLSeconds`.

This also fixes a real current problem: because there is no refresh, the token endpoint
**raises the access-token TTL to the session TTL** whenever the session TTL is longer
(`federated_auth.go:777-781`). So the configured 15-minute access token is issued with a
72-hour lifetime, and combined with the absence of revocation, a leaked token is good for
three days. Short access tokens plus refresh is the correct fix.

### 5.5 Consent screen — *net-new, M*

`authorize()` mints a code and redirects with no user approval and no scope display. That
is defensible for first-party apps and indefensible for third-party ones. Add a consent
step showing the requesting app and the scopes it wants, with remembered grants and a
per-client "skip consent (first-party)" flag so suite apps keep their current behaviour.

### 5.6 Logout propagation — *net-new, M*

There is no `end_session_endpoint`, no front-channel logout, and no back-channel logout, so
signing out of myidsan leaves every relying-app session alive. Implement RP-initiated
logout plus back-channel notification. This depends on Phase 2.3 — you cannot notify
sessions you never recorded.

### 5.7 Conformance testing — *M*

Run the OpenID Foundation's conformance suite for Basic OP and Config OP. Publish the
result. "Certified" is a procurement checkbox worth more than any feature bullet on this list.

**Phase 5 exit:** a customer can put software you have never heard of behind myidsan.

---

## Phase 6 — End-user product

Phases 0–5 satisfy the buyer. This phase satisfies the several hundred people who use it
every morning — which is what renewals turn on.

### 6.1 App launcher — *net-new, M*

A non-admin signs in and sees an empty dashboard reading "no menus". The dashboard renders
only admin sections the role grants, while `app_registry` sits full of relying apps with
base URLs that are surfaced to nobody. The launcher is *the* screen users associate with
SSO and it does not exist. Add per-app visibility rules so users see only what they can reach.

### 6.2 White-labelling — *net-new, M*

The login page's brand is a literal inline SVG with a hardcoded `myidsan` wordmark and a
hardcoded `#6f4d9d`; the console's is a hardcoded `<BrandLogo wordmark="myidsan" />` and a
CSS literal. There is no branding table, API, upload, or config key — a customer cannot put
their own logo on the page their staff see daily. For an on-prem IdP this is usually a
requirement, not a nice-to-have.

Add branding as configuration (logo upload, wordmark, accent colour, optional custom CSS)
applied to both the login page and the console.

### 6.3 Fix the login page properly — *M*

It is English-only, LTR-only, and light-theme-only — hardcoded strings across three
near-identical copies of an inline stylesheet — while the console behind it has four
languages, RTL, and three themes. This is the screen every federated user sees, and
myseliasan's SSO hop lands on it, so it matters beyond myidsan.

Give it the shared i18n dictionary, theme tokens, and RTL. Add a language switcher — today
a user cannot choose their language until *after* logging in. Deduplicate the three inline
stylesheets into the real asset pipeline that now exists.

### 6.4 Make RTL real — *M*

`dir="rtl"` is set for Arabic, and the CSS contains **zero** `[dir="rtl"]` rules; layout
relies entirely on the browser's flex/grid inference, while physical properties
(`margin-left`, `textAlign: right`, `borderLeft`) are used throughout. Arabic renders
visibly broken. Convert to logical properties (`margin-inline-start`, `text-align: start`,
`border-inline-start`). Your locale set says this market matters to you.

### 6.5 Console debt — *M*

- **URL routing.** Section is a `useState` plus a cookie — no deep links, no back button,
  nothing to paste into a ticket. The clearest "unfinished" signal in the product.
- **Users page cannot create, delete, or edit a user** — role assignment and enable/disable
  only. The most-used admin screen is read-mostly.
- Replace `window.confirm` / `window.prompt` for real decisions (make superadmin, delete
  app, MFA regenerate) with the modal system that already exists.
- Add an error boundary; a render throw currently white-screens the app.
- Add a documentation link — the console contains exactly one `href` and it's a social login.
- Converge on one table component; four implementations coexist with four different empty
  and loading behaviours.
- Re-adopt `rbac-standard.css` instead of the 2,831-line fork, and split the 4,272-line
  `App.js`. Both are due-diligence findings as much as maintenance ones.

**Phase 6 exit:** the product is pleasant for the people who did not buy it.

---

## Phase 7 — Commercial

### 7.1 Offline license enforcement — *net-new, M*

There is no license key, seat counting, expiry, feature gating, or activation anywhere in
the repo. The `LICENSE` is already dual — PolyForm Noncommercial plus a paid commercial
tier — so **selling is legally provided for**; what is missing is technical metering, not
legal basis.

Ship a signed offline license file: customer name, seat count, expiry, enabled features,
Ed25519-signed. Enforce at boot and on user creation, degrading to read-only rather than
refusing to start — locking an IT team out of their own identity provider over a license
check is worse for you than for them. No phone-home, per the non-goals.

### 7.2 Integration guide and sample relying party — *M*

Nothing in `docs/` explains the flow end to end for a customer's own application; the
in-console Apps walkthrough is good but only reachable after installing, and the only
working example is a whole application. Write the guide, and ship a minimal sample RP in
one or two languages so a prospect's developer can evaluate before installing.

Also state the multi-tenancy boundary here — one deployment per customer — rather than
letting an MSP discover it late.

### 7.3 Signed installers and SBOM — *S*

The Windows installer is unsigned, so SmartScreen warns on every install, and SBOM
generation is explicitly disabled in the goreleaser config. Both are routine procurement
friction with no engineering interest and a real cost in lost deals. Note the code-signing
certificate has a lead time — start it early, not in this phase.

### 7.4 Support policy — *S*

`SECURITY.md` is absent. No disclosure policy, no version-support matrix, no EOL policy, no
consolidated upgrade notes (the `compatibility` field exists per-change but is buried in
`changes/applied/*/change.json`). Cheap to write, and asked for more often than you would think.

**Phase 7 exit:** you can invoice.

---

## Verification

Each phase carries its own gate; none is complete on "the code is written."

| Phase | Verified by |
|---|---|
| 0 | The bench asserts all three federation paths; CI is green on a PR; `/swagger` renders with egress blocked |
| 1 | Restore a backup onto a clean host and log in with an **MFA-enrolled** account — this is what catches a missing at-rest key |
| 2 | Perform a role change, find it in the audit export, then revoke that user's session and confirm the next request fails |
| 3 | Password policy rejects at all four call sites; a superadmin without MFA is forced to enrol; spray one account from many IPs and get locked out |
| 4 | Two instances behind a load balancer with Redis keep a session alive; ZAP baseline is clean; k6 gives a documented concurrent-user number |
| 5 | *(skipped — suite chosen; see "The fork in the road")* |
| 6 | An end user with no admin role signs in and reaches an app from the launcher; Arabic renders correctly on the login page |
| 7 | An expired license degrades to read-only without data loss; a developer integrates using only the published guide |

The suite-wide lesson from [[suite-status-resume]] applies throughout: **boot and exercise
it, don't trust green.** Several items in this plan exist precisely because something
compiled, shipped, and was never run — the dead PKCE checkbox, the `user_session` table
nothing writes, and three federation paths whose own README says they were never tested.

---

## Sequencing notes

- **Phases 0 and 1 are the minimum before showing this to a paying prospect.** Together
  they are mostly ports and deletions.
- **Phases 2–4 can run in parallel** once Phase 1 lands; they touch largely disjoint code.
  2 and 3 are one reviewer conversation, so ship them together if you can.
- **Phase 5 should not start before the fork question is answered.** If the answer is
  "suite", skip to 6.
- **Phase 6.2 and 6.3 sell together.** White-labelling a login page that is English-only is
  half a feature.
- **Phase 7.3's code-signing certificate has a procurement lead time** — start it during
  Phase 0 even though the work lands last.

Run `docs-sync` and `i18n-sync` before committing any of this, per [[commit-push-docs-sync]] —
`i18n-sync` applies to every phase that touches frontend source, which is most of 6.
