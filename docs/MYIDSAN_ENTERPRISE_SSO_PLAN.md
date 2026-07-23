# myidsan Enterprise SSO Plan

**Status: DRAFT — approved study, implementation not started.**

Goal: extend myidsan beyond Google/GitHub with intranet-friendly enterprise identity —
LDAP / Active Directory, Kerberos SPNEGO, and generic OIDC — so the suite works as a
fully air-gapped SSO stack. myidsan stays an *identity broker*: upstream providers only
change **how a myidsan session is established**; the downstream federation to relying
apps (`/api/auth/authorize` → code → `/api/auth/token`) and per-app RBAC are untouched.

Non-goals: SAML (Phase 4, on concrete demand only), PAM (cgo — violates pure-Go rule),
RADIUS (not web SSO), trusted-header proxy auth (spoofing footgun; OIDC covers those IdPs).

---

## Current state (verified 2026-07-22)

- Google/GitHub are two hardcoded structs — `loginApi` fields
  (`apps/myidsan/apis/login.go:24-29`), fixed two-field config
  (`infra/login/login_models.go:21-24`). No provider interface, no registry.
- Federated users are deduped **by email only** (`login.go:222`, `login.go:286`);
  `UserLogin` (`domain/entities/user_login.go`) has **no provider/subject columns**.
  This is the same account-takeover pattern already fixed in myseliasan
  (`apps/myseliasan/services/rbac.go:248` — strict `SsoUserId`, never merge by email).
- Zero LDAP/SAML/Kerberos/OIDC code anywhere in the repo (grepped).
- Session issuance is shared and provider-agnostic: `IssueAuthCookies`
  (`domain/utils/middlewares/auth.go:163`), Email claim mandatory (`auth.go:147`).
- Schema changes: `infra/db/bootstrap` auto-adds missing columns from entity structs
  (`bootstrap.go:491`, NULL-safe read paths already exist for bool columns).
- Dependency rule: pure Go, no cgo, single static binary (documented repeatedly, e.g.
  `docs/MYIOTSAN_PLAN.md:1012`).

Library choices (all pure Go, no cgo):

| Need | Library | Notes |
|---|---|---|
| LDAP/AD | `github.com/go-ldap/ldap/v3` | de-facto standard, active |
| Kerberos SPNEGO | `github.com/jcmturner/gokrb5/v8` | pin version; maintenance is slow (Sarama flagged it unmaintained Dec 2025) but the server-side accept-ticket path is small and protocol-stable — treat as frozen code |
| OIDC RP | `github.com/coreos/go-oidc/v3` | thin layer over `golang.org/x/oauth2` (already a dep) |
| SAML (Phase 4 only) | `github.com/crewjam/saml` ≥ latest | only on demand; check advisories at adoption time |

---

## Phase 0 — Provider seam + identity schema (prerequisite, includes a security fix)

### 0.1 `IdentityProvider` interface + registry (`infra/login/provider.go`, new)

```go
// Identity is the normalized result of any federated login.
type Identity struct {
    Provider      string   // registry key: "google", "github", "ldap", "kerberos", "oidc:<key>"
    Subject       string   // stable unique id AT the provider (Google sub, GitHub id,
                           // AD objectGUID, Kerberos principal, OIDC sub) — NEVER email
    Email         string
    EmailVerified bool
    Name          string
    GivenName     string
    FamilyName    string
    Picture       string
    Groups        []string // provider group names/DNs (empty for social) — feeds role mapping
}

// RedirectProvider is a browser-redirect login method (OAuth2/OIDC).
type RedirectProvider interface {
    Key() string         // stable lowercase key, used in /api/login/{key} + /api/callback/{key}
    DisplayName() string // button label
    Login(w http.ResponseWriter, r *http.Request)
    Callback(r *http.Request) (*Identity, error)
}
```

Registry: `map[string]RedirectProvider` built at startup in `NewLoginApi`. Replace the
four per-provider handlers/routes in `login.go:68-76` with two generic mux routes using
a path variable:

```
GET /api/login/{provider}      -> registry lookup -> setOAuthContinue(provider) -> p.Login
GET /api/callback/{provider}   -> registry lookup -> p.Callback -> upsertFederated -> session -> consumeOAuthContinue
```

Adapt `GoogleLogin`/`GithubLogin` to implement `RedirectProvider` (Google `Subject` =
userinfo `id`; GitHub `Subject` = numeric `id` as string — both already fetched, just
currently discarded). The duplicated google/github callback blocks (`login.go:210-315`)
collapse into ONE shared path.

- `/api/login/providers` (`login.go:82`) returns the registry:
  `[{key, displayName, kind: "redirect"|"form"}]` (SPA + server page render from this;
  keep returning the old `{google:bool, github:bool}` shape alongside for one release
  so an un-rebuilt SPA doesn't break).
- `socialButtonsHTML` (`apps/myidsan/apis/federated_auth.go:293`) iterates the registry
  instead of the two hardcoded providers. Keep the "only render if actually configured"
  behavior — that guard exists for air-gapped installs.
- Config: keep `login.google`/`login.github` blocks working as-is (mapped into the
  registry at startup); new provider types get their own blocks/entities (per phase).

### 0.2 Identity columns + strict-subject dedup (SECURITY FIX)

Add to `UserLogin` (`domain/entities/user_login.go`) — bootstrap auto-migrates:

```go
SsoProvider string `json:"ssoProvider" ...` // "" for local-only accounts
SsoSubject  string `json:"ssoSubject"  ...` // stable id at the provider
```

New shared upsert in `services/user_login.go` (replaces the per-provider create blocks):

```
UpsertFederated(ctx, id Identity) (*UserLogin, error):
  1. lookup by (sso_provider, sso_subject)            -> found: update name/pic, login
  2. miss -> lookup by email:
     a. found AND row.SsoSubject == ""                -> one-time legacy claim: stamp
        provider+subject onto the row, log the claim  (backfills pre-upgrade social users
        and lets an admin pre-provision by email)
     b. found AND row.SsoSubject != (provider,subject)-> REFUSE with explicit error
        ("account with this email belongs to another identity") — this closes the
        email-merge takeover hole
  3. full miss -> create with UserRoleId 0 (pending clearance), IsActive true
```

Use `repo.Get` + Equal filters for the (provider,subject) lookup — NOT `GetByForeign`
(hardcodes limit 1 on the wrong axis; known infra gotcha). Unit tests must cover 2b
(the takeover attempt) and the legacy-claim path.

Local password rules: an account with `SsoProvider != "" && Userpwd == ""` stays a
third-party-only account (existing `ErrThirdPartyOnlyAccount` path).

**Deliverable:** 1 PR. Pure refactor + schema + dedup fix, no new login methods.
Regression-test Google/GitHub login and the myseliasan federation round-trip.

---

## Phase 1 — LDAP / Active Directory bind

Covers AD, Samba AD, FreeIPA, OpenLDAP, 389-ds. Form credentials → LDAP bind → group→role mapping.

### 1.1 Dependency + client (`infra/login/ldap.go`, new)

`github.com/go-ldap/ldap/v3`. Flow per authentication:

```
1. Dial ldaps://host:636 (or StartTLS on 389) — TLS REQUIRED, optional pinned CA
   (reuse the myseliasan sso.caCertPath pattern), 10s dial/op timeouts.
2. Bind as service account (read-only bind DN + password).
3. Search: base=<baseDN>, scope=sub, filter built from a template, default
   (&(objectClass=user)(|(sAMAccountName=%q)(uid=%q)(mail=%q)))  — %q escaped via
   ldap.EscapeFilter. Require EXACTLY ONE result (0 -> invalid credential; >1 -> error).
4. REFUSE empty/whitespace password BEFORE bind (empty password = unauthenticated bind
   "success" on many servers — classic bypass).
5. Bind as the found user DN with the supplied password -> success = authenticated.
6. Read attributes on the user entry: mail, displayName, givenName, sn,
   memberOf (AD) / configurable group attr, objectGUID (AD, binary -> uuid string) or
   entryUUID (RFC 4530). Subject = that GUID/UUID; fallback to DN only if neither exists
   (warn loudly — DN changes on OU moves).
7. Return Identity{Provider:"ldap", Subject:guid, Email:mail, Groups:memberOf DNs}.
```

Not a `RedirectProvider` — it's a credential check. Parallel seam to
`AuthenticateDefault` (`services/user_login.go:67`).

### 1.2 Config: DB-backed + settings UI (not config.json)

Directory settings change often and deserve a test button — follow the
`AppAuthConfig` precedent (DB entity + RBAC-gated API + "Federation" SPA menu).

New entity `DirectoryConfig` (single row): `Enabled, Host, Port, UseStartTLS, CaCertPem,
BindDn, BindPassword (encrypted at rest via infra/atrest), BaseDn, UserFilter,
GroupAttr, SubjectAttr, DisplayLabel ("Sign in with <label>")`.

New entity `FederatedGroupMapping`: `Provider ("ldap" now, OIDC keys later), GroupName
(DN or name), RoleId, Priority`. Resolution: highest-Priority match wins; **no match →
role 0 (pending clearance)** — composes with the existing gate. Groups re-evaluated on
every login; role changes apply at next login (sessions deliberately don't revalidate
role, `auth.go:373`).

Decide at build time: whether LDAP role mapping OVERRIDES a manually assigned role on
every login (directory is authoritative — recommended, with an `Authoritative bool`
toggle on `DirectoryConfig`) or only sets the initial role.

APIs (RBAC-gated, superadmin): `GET/PUT /api/directory-config`,
`POST /api/directory-config/test` (service bind + sample search, returns found-user
count + sample attrs, NEVER stores), CRUD `/api/federated-group-mapping`.

### 1.3 Login endpoints + UI

- `POST /api/login/ldap` (JSON, SPA) and LDAP branch in the server-rendered
  `loginPost` (`federated_auth.go:316`) — render a second "Domain account" tab/toggle
  on both login surfaces when enabled. Keep local + LDAP forms distinct: silent
  fallthrough (try local then LDAP) makes lockout/audit ambiguous.
- Route LDAP attempts through the same `LoginSecurity` per-IP lockout counters as local
  login, and cap concurrent in-flight LDAP binds (protect the DC from a spray).
- Success → `UpsertFederated` → role from group mapping → `IssueAuthCookies`. The
  federated flow works unchanged (LDAP login mid-`/api/auth/authorize` redirect included).

### 1.4 Metrics + audit ("instrument what fails silently")

Counters: `ldap_bind_failures_total{stage=service|user}`, `ldap_search_ambiguous_total`,
`ldap_unreachable_total`. Audit log entries for: legacy email-claim (0.2.2a), refused
merge (2b), authoritative role change applied by group mapping.

### 1.5 Testing

- Unit: filter escaping, empty-password refusal, >1 result refusal, GUID decode,
  group→role priority resolution.
- Integration: `docker run` **Samba AD DC** container (gives LDAP *and* Kerberos for
  Phase 2 from the same realm) + OpenLDAP (osixia) for the non-AD path. Script under
  `tools/` like the k6/zap patterns.

**Deliverable:** 1 PR (backend + settings UI + login UI). i18n-sync (new UI strings,
en/ms/zh/ar) + docs-sync before commit.

---

## Phase 2 — Kerberos SPNEGO (zero-prompt Windows/domain SSO)

Domain-joined machine → browser sends `Negotiate` ticket → signed in without typing anything.

### 2.1 Dependency + endpoint

`github.com/jcmturner/gokrb5/v8` (pin exact version; server-side only: `keytab`,
`service`, `spnego` packages). Config block (file-based is fine here — keytab is an
ops-provisioned artifact, not a UI setting): `kerberos.enabled`,
`kerberos.keytabPath`, `kerberos.servicePrincipal` ("HTTP/myidsan.corp.local"),
`kerberos.stripRealm` (bool), `kerberos.onlyRealms []string`.

New endpoint (NOT a change to the shared auth middleware — keep the blast radius small):

```
GET /api/login/kerberos?continue=<path>
  no Authorization header -> 401 + WWW-Authenticate: Negotiate   (browser retries with ticket)
  Negotiate token present -> spnego validate against keytab
     ok  -> principal "alice@CORP.LOCAL"
     bad -> 302 back to /api/auth/login?error=sso_failed  (never a dead-end 401 page)
```

### 2.2 Identity resolution — Kerberos authenticates, LDAP describes

A ticket proves *who* the user is but carries no email/groups (PAC parsing is
possible in gokrb5 but fragile — don't). Resolution:

- **If Phase 1 LDAP is enabled (the expected pairing):** service-bind search by
  `sAMAccountName = principal-without-realm` → same attributes, same GUID subject,
  same group→role mapping. Kerberos and LDAP logins for the same person converge on
  the SAME (provider="ldap", subject=GUID) identity — no duplicate accounts.
- **Standalone fallback:** Identity{Provider:"kerberos", Subject:full principal,
  Email: principal-as-email only if `stripRealm` mapping configured}. Document that the
  Email claim is mandatory (`auth.go:147`), so standalone mode requires a
  principal→email rule; recommend requiring LDAP instead.

### 2.3 Login page UX

Both login surfaces get a "Sign in with Windows (SSO)" button → navigates to
`/api/login/kerberos?continue=…`. Navigation (not fetch) — browsers only do the
Negotiate dance on top-level requests reliably. Optional later polish: silent
auto-attempt probe with cookie-marked backoff; NOT in the first cut.

### 2.4 Ops documentation (deploy/README addition — this is half the feature)

- SPN + keytab: `setspn -S HTTP/myidsan.corp.local svc-myidsan` then
  `ktpass ... /out myidsan.keytab` (AD) or `samba-tool spn add` + `samba-tool domain
  exportkeytab` (Samba). Keytab file perms 0600; path in config.
- Clock skew < 5 min (Kerberos hard requirement).
- Browser trust: Edge/Chrome — Intranet zone or `AuthNegotiateAllowlist` policy;
  Firefox — `network.negotiate-auth.trusted-uris`. FQDN (not IP) in the URL bar.
- Failure modes table: wrong SPN → `KRB_AP_ERR_MODIFIED`; skew → `KRB_AP_ERR_SKEW`;
  each mapped to the metric + log line to check.

Metrics: `kerberos_ticket_rejects_total{reason}`, `kerberos_ldap_lookup_failures_total`.

### 2.5 Testing

Samba AD DC container from Phase 1: create realm, export keytab, `kinit` a test user in
a client container, curl with `--negotiate`. Unit tests with gokrb5's test vectors for
the token-parse path.

**Deliverable:** 1 PR. Depends on Phase 1 (shares realm + role mapping).

---

## Phase 3 — Generic OIDC relying party

One integration → Keycloak, Authentik, Authelia, Zitadel, ADFS 2016+, Entra ID, Okta.

### 3.1 Dependency + provider (`infra/login/oidc.go`, new)

`github.com/coreos/go-oidc/v3` on top of the existing `golang.org/x/oauth2`.
Implements `RedirectProvider` — slots straight into the Phase 0 registry, routes come
for free (`/api/login/oidc:<key>`, `/api/callback/oidc:<key>` — or flatten to the key
itself; decide during build, keys must not collide with built-ins).

Config: a LIST (multiple IdPs at once), file-based `login.oidc[]`:
`{key, displayName, issuerUrl, clientId, clientSecret (env-overridable like
GOOGLE_CLIENT_SECRET), scopes (default ["openid","profile","email"]), caCertPath,
groupsClaim, insecureSkipEmailVerified}`.

- Discovery via `issuerUrl/.well-known/openid-configuration` at startup (intranet URL —
  fine air-gapped); fail soft like half-configured Google today (warn + skip provider,
  don't block boot). Custom `http.Client` with pinned CA for discovery/JWKS/token.
- **PKCE S256 + nonce on this leg** (new code, no excuse not to); reuse
  `NewOAuthState`/`ValidateOAuthState` (`infra/login/config.go:46`) for state.
- Verify id_token (issuer, audience, exp, nonce, sig via JWKS). Identity: Subject =
  id_token `sub` (Provider key includes the config key, so two IdPs can't collide),
  email from claims — require `email_verified` unless the per-provider override is set
  (Keycloak on intranet often leaves it false).
- `groupsClaim` (e.g. "groups", "roles") → `Identity.Groups` → the SAME
  `FederatedGroupMapping` table from Phase 1 (Provider = "oidc:<key>").

### 3.2 Cleanup opportunity (same PR or follow-up)

Google IS an OIDC provider — fold `GoogleLogin`'s hand-rolled userinfo call into the
OIDC path (issuer `https://accounts.google.com`) and delete `infra/login/google.go`.
GitHub is NOT OIDC and stays bespoke.

### 3.3 Testing

Keycloak container: realm + client + test users + a groups mapper; assert full
round-trip including group→role. Negative tests: tampered id_token, wrong nonce,
issuer mismatch, unverified email.

**Deliverable:** 1 PR. Depends only on Phase 0 (can land before Phase 2 if priorities shift).

---

## Phase 4 — SAML 2.0 SP (parked — build only on concrete demand)

Modern intranet IdPs all speak OIDC; SAML adds a heavy XML-dsig attack surface
(crewjam/saml assertion bypass pre-0.4.9; 2024-12 Go XML round-trip class — both
patched, but the class keeps giving). If a deployment truly requires it (old ADFS,
Shibboleth): `crewjam/saml` as SP, `/api/saml/metadata` + `/api/saml/acs`, NameID →
Subject, attribute→groups → same mapping table, registered as a redirect provider.
Re-check advisories and library maintenance at adoption time. Not scheduled.

---

## Cross-cutting rules (all phases)

- **Air-gap:** every new provider must work with zero internet egress; no external
  assets on login pages (CSP `default-src 'self'` stands).
- **Pure Go:** the three chosen libs are cgo-free. No OS GSSAPI/SSPI bindings.
- **Pending clearance stands:** no provider auto-grants access; only an explicit group
  mapping or a superadmin assigns roles.
- **Secrets:** bind password / client secrets encrypted at rest (`infra/atrest`) when
  DB-stored; env-overridable when file-stored; never returned by any GET API
  (follow `HasClientSecret` pattern).
- **Gates before every commit:** docs-sync always; i18n-sync when FE strings change
  (en/ms/zh/**ar**). ZAP rescan of myidsan after Phases 1–3 land (new endpoints).
- **Relying apps:** zero changes required in myseliasan/mymatasan/myiotsan for any phase.

## Suggested PR sequence

| PR | Content | Risk |
|---|---|---|
| 1 | Phase 0: provider registry + `SsoProvider/SsoSubject` + strict dedup (security fix) | Medium (touches live login paths) — regression-test Google/GitHub + myseliasan SSO round-trip |
| 2 | Phase 1: LDAP/AD + group→role mapping + settings UI + test-connection | Low (additive) |
| 3 | Phase 2: Kerberos SPNEGO + ops docs + Samba test harness | Low (additive, isolated endpoint) |
| 4 | Phase 3: OIDC multi-provider (+ optional Google fold-in) | Low (additive) |
