# Module: apps/myidsan/apis/login.go

## Purpose

Provides authentication endpoints for local credentials, LDAP/Active Directory
credentials, Kerberos SPNEGO SSO, and any registered redirect-style federated
identity provider (`infra/login.Registry`, see `infra/login/provider.go.md`).

## Responsibilities

- Handles local credential login via `POST /api/login/default`.
- Handles local account registration via `POST /api/login/default/register` — the password is validated against the configured policy (`services.ValidatePassword`, see `services/password_policy.go.md`) inside `services.RegisterLocal` before the account is created.
- Publishes the effective password-strength policy at `GET /api/login/password-policy` (public, `handler.passwordPolicy`) — `{minLength, requireUpper, requireLower, requireDigit, requireSymbol, blockCommon}` — so the sign-up/change-password forms can state the rules before the user types, rather than a hardcoded hint that can drift from the configured policy (which is exactly what "at least 8 characters" had become). `NewLoginApi`'s `LoginApiOptions` gained a `PasswordPolicy config.EffectivePasswordPolicy` field, resolved once from `deps.Config.PasswordPolicy.Effective()` in `apps/myidsan/app/app.go` and stored on the handler.
- Handles LDAP/Active Directory credential login via `POST /api/login/ldap` (`ldapLogin`) — a JSON credential POST, not a browser redirect, so it is a fixed route rather than a member of the redirect-provider registry. Disabled (`directory == nil` or the configured directory is off) responds `ErrLimitedAccess`. On success it calls `services.IDirectoryService.AuthenticateLdap` (see `services/directory.go.md`) and issues the session through the same `issueLocalSession` local login uses.
- Handles Kerberos SPNEGO SSO via `GET /api/login/kerberos` (`kerberosLogin`) — see "Kerberos SPNEGO SSO" below.
- Sets HttpOnly JWT session cookies for successful local/LDAP/Kerberos login/register and federated callbacks.
- Sets a readable CSRF cookie that clients echo in `X-CSRF-Token` for unsafe authenticated requests.
- Clears session cookies through logout.
- `NewLoginApi` builds the provider registry via `login.BuildRegistry(oAuth2Conf)` (Google/GitHub register only when both `ClientId` and `ClientSecret` are present) and **returns it**, so `apps/myidsan/app/app.go` can thread the same registry into `NewFederatedAuthApi` — one registry, one provider list, shared by the server-rendered login page, the SPA, and the OAuth routes. `NewLoginApi`'s remaining parameters are now collected into a `LoginApiOptions` struct (`Directory services.IDirectoryService`, `Kerberos *login.KerberosAuthenticator`, `KerberosLabel string`, `Guard *sharedapis.LoginGuard`, `Metrics telemetry.Metrics`, `Mfa services.IMfaService`, `Store cache.Store`, `WebAuthn services.IWebAuthnService`, `Reset services.IPasswordResetService`, `Audit services.IAuditService`, `Sessions services.ISessionService`, `TrustedProxies []string`, `PasswordPolicy config.EffectivePasswordPolicy`) — every field may be zero (tests pass the empty struct; LDAP/Kerberos disabled / lockout off / metrics not wired / MFA not armed / reset request endpoint a no-op / audit+session recording a no-op). `KerberosLabel` defaults to `"Windows (SSO)"` when blank and `Kerberos` is non-nil. `Store` plus either `Mfa` or `WebAuthn` arms the `*mfaChallenger` (see "Second-Factor (MFA) Login Challenge" below); all absent leaves password-only behaviour unchanged. `WebAuthn` is held both on the challenger (for the "does this account owe a second factor" question) and directly on `loginApi` (the two pre-session security-key legs run the ceremony itself, which the challenger has no business knowing how to do). `Reset` (nil-safe) enables `POST /api/login/forgot` (see "Account Recovery (Forgot Password)" below). `TrustedProxies` is parsed once via `middlewares.ParseTrustedProxies` and stored on the api so every audit/session-recording call resolves the same client address.

## Login/Session Auditing (Phase 2)

Every credential surface in this file now records to `services.IAuditService` (nil-safe —
`recordAudit` is a no-op when `m.audit == nil`, so tests and minimal wiring are unaffected):

- `recordLoginFailure(w, r, attempted, method, reason)` — replaces the old parameterless
  `recordLoginFailure(w, r)`. Records `services.ActionLoginFailure` with `ActorEmail`/
  `TargetId` set to the ATTEMPTED identifier (not resolved to a real account — resolving it
  would turn the trail into its own account-existence oracle) and `Metadata: {method}`.
  When the failure trips the lockout, also records `services.ActionLoginLockout`. Called
  from `defaultLogin` (attempted username, `MethodLocal`) and `ldapLogin` (attempted
  username, `MethodDirectory`). Both `recordLoginFailure` and the sibling below now share a
  `recordCredentialFailure(r, action, attempted, method, reason)` helper that writes the
  entry, delays the response, and advances the shared lockout.
- `recordMfaChallengeFailure(r, reason)` — a bad second-factor code (`mfaLogin`) is now
  audited under its **own** action, `services.ActionMfaChallenge`, not
  `services.ActionLoginFailure` — filed as a plain login failure, a code being ground is
  indistinguishable from a password being guessed, and the two are not the same incident:
  whoever is here has already cleared the password, a later and more urgent stage of an
  intrusion. No identifier is attached — this step presents a challenge token, not a
  username. Still advances the same `LoginGuard` lockout a bad password does. A separate,
  unauthenticated-token failure (unknown/expired/rebound) records `services.ActionMfaChallenge`
  directly (not through `recordMfaChallengeFailure`, since it does not count against the
  lockout) with `Detail: "second-factor challenge expired, exhausted, or presented from a
  different client"`.
- `recordLoginSuccess(r, user, method)` — called once a session has actually been issued
  (`issueLocalSession`, `kerberosLogin`, `providerCallback`, `mfaLogin`'s completion), never
  when a password merely verified — a password-correct login that stops at the MFA
  challenge has not signed anyone in. Records `services.ActionLoginSuccess` with the actor
  from the resolved `*entities.UserLogin` and `Metadata: {method}`.
- `loginMethodForUser(user)` derives the sign-in method (`local`/`ldap`/`kerberos`/`oidc`/
  `social`) from the account's `SsoProvider` binding, used where the original request that
  started an MFA challenge is long gone — labelling a directory user's MFA completion as
  `local` would misreport how they authenticate.
- `recordFederatedLoginFailure(r, providerKey, attempted, reason)` — audits a refused
  redirect-provider (`login.oidc[]`/Google/GitHub) sign-in, called from all four rejection
  paths in `providerCallback` (below). Records `services.ActionLoginFailure` /
  `OutcomeDenied` with `ActorEmail`/`TargetId` set to `attempted` (may be empty — a bad
  `state` or a failed code exchange fails before any identity is known) and
  `Metadata: {method: federatedMethodForKey(providerKey), provider: providerKey}`.
  Deliberately does **not** go through `recordLoginFailure`: that helper also advances the
  per-IP/per-account `LoginGuard` lockout, and a federated failure is not password guessing
  — the credential was checked at the IdP, so counting it would let a misconfigured provider
  (clock skew, an unverified email, a rotated client secret) lock legitimate users out of an
  address they never guessed a password from. A live bench against a real Keycloak 26 found
  this gap: every refused SSO sign-in was previously invisible on the audit page while a
  refused password login was recorded — see `TestFederatedCallbackAuditsARefusedSignIn` in
  `apis/login_federated_audit_test.go.md`.
- `federatedMethodForKey(providerKey)` classifies a provider key into `services.MethodSocial`
  (`""`, `"google"`, `"github"`) or `services.MethodOIDC` (anything else — a configured
  `login.oidc[].key`), mirroring `loginMethodForUser` but usable before any account has been
  resolved.
- `recordKerberosLoginFailure(r, attempted, reason)` — the Kerberos counterpart of
  `recordFederatedLoginFailure`, called from all three rejection paths in `kerberosLogin`
  (see "Kerberos SPNEGO SSO" below). Records `services.ActionLoginFailure` / `OutcomeDenied`
  with `Metadata: {method: services.MethodKerberos, provider: login.KerberosProviderKey}`.
  Deliberately does **not** advance the `LoginGuard` lockout, for the same reason as the
  federated case: a ticket is verified cryptographically against the keytab rather than
  guessed, and a keytab gone stale after a machine-account password rotation would otherwise
  lock out every domain user at once. A live bench against a real Samba AD DC (realm
  `KOPI.TEST`) found the same gap here as the federated case: a rejected SPNEGO ticket left
  no audit record at all — only a Prometheus counter via `recordFederatedLogin` — while a
  rejected LDAP password login was recorded. The `ErrKerberosNoToken` challenge path
  deliberately does **not** call this helper — see `TestKerberosChallengeIsNotAudited` in
  `apis/login_kerberos_audit_test.go.md`.
- `defaultLogout` records `services.ActionLogout` by reading claims straight off the cookie
  via `m.auth.ClaimsFromRequest(r)` rather than the request context — logout is a public
  route (must work even with an expired session) that never passes through the auth
  middleware, so the context carries no claims. Recorded before `ClearAuthCookies`, while
  there is still an identity to attribute the event to.
- `issueSessionCookies` now also **pre-generates the session id** (`newFederatedOpaqueToken`)
  when `m.sessions != nil`, so this app can index the session it is about to issue —
  `AuthMidware.IssueAuthCookies` mints one only when the field is empty and never reports
  it back, so without this the caller could not know which session it just created,
  meaning an unindexed session no administrator could list or revoke. After the cookies are
  set, `recordSession` calls `ISessionService.Record` with the resolved client IP/user
  agent and the claim's `ExpiresAt`.
- One generic route pair serves every registered redirect provider: `GET /api/login/{provider}` (`providerLogin`) and `GET /api/callback/{provider}` (`providerCallback`), matched by `mux` against `{provider:[a-z][a-z0-9_.:-]*}` — registered *after* the fixed `/default*`/`/ldap`/`/kerberos`/`/providers` routes so those still win the match. An unregistered provider key 404s with `ErrLimitedAccess` (`"<key> login is not configured"`).
- Prevents local credential takeover of third-party-managed accounts.
- Exposes `GET /api/login/providers` (`listProviders`) — public, no auth — returning the registry's authoritative `list: [{key, displayName, kind}]` (render order) plus legacy `{"google": bool, "github": bool}` booleans for older SPA builds that have not switched to reading `list`. `kind` is `"redirect"` for every registry provider, `"form"` for the directory option, and `"redirect"` again for Kerberos (`login.KerberosProviderKey`, `resp["kerberos"] = true`, displayed as `m.kerbLabel`) — a redirect kind because, unlike LDAP, the SPNEGO 401/Negotiate exchange happens transparently on browser navigation, with no credential form. The directory entry (`{key: login.LdapProviderKey, displayName: <configured label>, kind: "form"}`, plus `resp["ldap"] = true`) is added only when `directory.LoginOption(ctx)` reports it enabled, and the Kerberos entry only when `m.kerberos != nil` — so a disabled directory/Kerberos config renders neither a button nor a dead account-type choice on either login surface.

## Kerberos SPNEGO SSO (`kerberosLogin`)

`GET /api/login/kerberos` is browser navigation, not an XHR credential POST, so
failures redirect back to the login page rather than returning a JSON error
body or a dead-end `401` page:

1. Not configured (`m.kerberos == nil`): `ErrLimitedAccess` — **no challenge is
   issued**, since challenging an unconfigured endpoint would pop a Windows
   credential prompt in the browser for nothing.
2. `m.kerberos.Negotiate(r)` (see `infra/login/kerberos.go.md`):
   - `ErrKerberosNoToken` → `login.KerberosChallenge(w)`: `401` +
     `WWW-Authenticate: Negotiate`, making a domain-joined browser retry with a
     service ticket.
   - Any other error (rejected ticket, disallowed realm) → logged, recorded via
     `recordFederatedLogin`/`kerberosResultLabel` **and** audited via
     `recordKerberosLoginFailure(r, "", err.Error())` (empty `attempted` — no
     principal was resolved), then redirected via `redirectKerberosFailure`.
3. `resolveKerberosUser(r, principal)`: if a directory is configured, resolves
   through `services.IDirectoryService.ResolveDirectoryUser` (the directory
   describes the ticket-verified principal — see `services/directory.go.md`);
   `ErrDirectoryDisabled` falls back to
   `login.StandaloneKerberosIdentity(principal)` +
   `IUserLoginService.UpsertFederated` for directory-less installs. Any other
   resolution error (not found, identity conflict, inactive account) is also
   metered and audited (`recordKerberosLoginFailure(r, principal.Username+"@"+
   principal.Realm, err.Error())`) and redirected.
4. Success calls the shared `issueSessionCookies` (below) and redirects
   `302` to the pending `continue` target. A failure here is also audited
   (`recordKerberosLoginFailure(r, user.Email, "session issue failed: "+
   err.Error())`) before the `ErrInternalServerError` response.
5. `redirectKerberosFailure` always lands on `/api/auth/login?error=sso_failed`
   (preserving `continue`), which `federated_auth.go`'s `loginPage` renders as
   an inline "Single sign-on failed..." message — see
   `apps/myidsan/apis/federated_auth.go.md`.

- `kerberosResultLabel(err)` maps `login.ErrKerberosRejected` →
  `ticket_rejected`, `login.ErrKerberosRealmNotAllowed` → `realm_refused`,
  `login.ErrLdapUnreachable` → `unreachable`, `login.ErrLdapInvalidCredential`
  (from `ResolveDirectoryUser`: a verified principal with no directory entry) →
  `not_in_directory`, `services.ErrFederatedIdentityConflict` →
  `identity_conflict`, `services.ErrInactiveAccount` → `inactive`, anything
  else → `error`.
- `issueSessionCookies(w, r, user)` is a new shared helper: it signs and sets
  the auth/CSRF cookies **without writing a response body**. `issueLocalSession`
  (local/LDAP JSON login) now calls it and then writes
  `{ result: { ok: true } }` itself; `kerberosLogin` calls it and then performs
  the `302` redirect — one cookie-issuing path, two different responses for a
  JSON API vs. a browser-navigation flow.

## Second-Factor (MFA) Login Challenge

When `LoginApiOptions.Mfa` and `.Store` are both set, `NewLoginApi` builds a
`*mfaChallenger` (see `apis/mfa_challenge.go.md`) and every successful **password**
credential check — `defaultLogin` and `ldapLogin` — routes through
`completeLoginOrChallenge` instead of calling `issueLocalSession` directly:

- No confirmed factor on the account (`mfaChallenger.required` false, or MFA not
  armed) → session issued as before, `{ result: { ok: true } }`, unchanged
  behaviour.
- A confirmed factor exists → **no session cookie is set**. A challenge token is
  minted (`mfaChallenger.issue`), the `issued` outcome is recorded
  (`recordMfaChallenge`), and the response is `{ mfaRequired: true, mfaToken:
  "<opaque>", mfaMethods: [...] }`. `mfaMethods` (`mfaChallenger.methods`, see
  `apis/mfa_challenge.go.md`) lists which factor kinds this specific account can present
  (`"webauthn"` before `"totp"` — a key is both stronger and quicker than typing a code),
  so the client can prompt for a code, ask for a key, or offer a choice, instead of always
  assuming TOTP and stranding a user whose only factor is a security key. An older client
  that ignores the field keeps working unchanged — the TOTP leg's behaviour did not move.

`POST /api/login/mfa` (`mfaLogin`, public — there is no session yet, the token
itself is the short-lived, single-use, client-bound authorization to complete this
login) redeems `{mfaToken, code}` via `mfaChallenger.redeem`, which now also returns
`usedRecovery`:

- `services.ErrMfaBadCode` → `401` "invalid verification code", the `failed`
  metric is recorded, and the attempt counts toward the per-IP `LoginGuard` lockout
  exactly like a bad password would — via `recordMfaChallengeFailure` (above), which
  records `services.ActionMfaChallenge` rather than `services.ActionLoginFailure`.
- Any other failure (unknown/expired/rebound token, or an internal error) → `401`
  "your verification session expired — sign in again", metric `expired`, and now also
  records `services.ActionMfaChallenge` / `OutcomeDenied` directly. The two cases are
  **not** distinguished in the response, to avoid an oracle that would tell an attacker
  whether a guessed token exists.
- Success reloads the resolved account (`loadActiveUser`, re-checking `IsActive` in
  case it was disabled during the challenge window), records `success`, calls
  `recordRecoveryLogin(r, user, usedRecovery)` — when the second factor that cleared was a
  recovery code, records `services.ActionMfaRecovery` (`surface: "login"`), since a sign-in
  completed with break-glass looks completely ordinary otherwise, and the codes are finite —
  then calls `issueLocalSession` — the session `completeLoginOrChallenge` withheld.

`mfaLogin` checks `guardLocked` before doing any work, same as `defaultLogin`/
`ldapLogin`. Kerberos SPNEGO SSO and OAuth/OIDC callbacks deliberately do **not**
route through `completeLoginOrChallenge` — their upstream IdP owns factor policy
(see `docs/MYIDSAN_MFA_PLAN.md` §5).

## Second-Factor Login: Security Keys (WebAuthn)

The security-key twin of the TOTP leg above, added alongside it rather than replacing it —
an account may hold either or both, and whichever it holds is what `mfaMethods` (above)
advertises. Two legs, because the ceremony needs a server-issued challenge before an
authenticator can sign anything:

- `POST /api/login/mfa/webauthn/begin` (`webauthnLoginBegin`, public) — body
  `{mfaToken}`. **Peeks** the pending challenge (`mfaChallenger.peek`) rather than
  consuming it: the token must survive to the `finish` leg, since the security-key
  ceremony is two round trips against the same login attempt. Peeking still re-checks the
  client fingerprint, so a token lifted from one browser cannot be driven from another.
  Returns the assertion options from `services.IWebAuthnService.BeginAssert`, keyed by
  `webauthnLoginStateKey(mfaToken)` (`"login:" + mfaToken`) so two concurrent sign-in
  attempts for the same account cannot consume each other's ceremony state.
- `POST /api/login/mfa/webauthn/finish` (`webauthnLoginFinish`, public) — body
  `{mfaToken, credential}`. Peeks the same token again, verifies the assertion via
  `IWebAuthnService.FinishAssert`, and only **now** spends the challenge token
  (`mfaChallenger.consume`) — the same single-use guarantee the TOTP path's `redeem`
  gives, just deferred to the leg that actually proves the factor. A non-advancing
  signature counter (`note != ""` — the library's clone signal, but ambiguous because most
  platform authenticators and every synced passkey legitimately report `0` forever) does
  **not** refuse the sign-in; it records `services.ActionWebAuthnClone` against the account
  so an investigator has a durable trace. On success, calls `guardSuccess`,
  `recordMfaChallenge("success")`, and `issueLocalSession` — the session
  `completeLoginOrChallenge` withheld, exactly like `mfaLogin`'s completion.

Both legs check `guardLocked` first (same lockout `defaultLogin`/`ldapLogin`/`mfaLogin`
share) and 401 with a generic "your verification session expired" on any ceremony failure,
never distinguishing an unknown/expired token from a rejected assertion — the same
anti-oracle reasoning as the TOTP path. Requires `LoginApiOptions.WebAuthn` to be set and
`Enabled()`; either absent answers `ErrLimitedAccess` ("security keys are not configured").

## Account Recovery (Forgot Password)

`POST /api/login/forgot` (`forgotPassword`, public) accepts `{username}` (email or
username) and **always** returns the same generic shape,
`{ok: true, mailEnabled: <bool>}` — it never reveals whether the identifier matched
an account, so it cannot be used as an account-enumeration oracle:

- Checked against `guardLocked` first (the same per-IP `LoginGuard` credential
  surfaces share), so the endpoint cannot be hammered to fingerprint accounts either.
- Delegates to `services.IPasswordResetService.Request` (see
  `services/password_reset.go.md`), which resolves the identifier to a real **local**
  account only; a miss, an unknown identifier, or a federated/SSO-only account is
  silently a no-op. A storage error from `Request` is logged but also swallowed —
  surfacing it would itself be an oracle.
- `mailEnabled` reflects `m.reset.MailEnabled()` — global config state (`smtp.enabled`
  + relay host present), not whether *this* account got an email — so the SPA can
  safely say "check your email" vs. "an administrator has been notified" without
  leaking anything about the specific identifier.
- `requestOrigin(r)` reconstructs the public `scheme://host` the client reached this
  instance on (TLS-derived scheme, honouring `X-Forwarded-Proto` from a terminating
  proxy) so the service can build an absolute self-service reset link when mail is
  enabled.

This is the SPA side of account recovery; the server-rendered equivalent
(`GET/POST /api/auth/forgot`, `GET/POST /api/auth/reset`) lives in
`apps/myidsan/apis/federated_auth.go` — see that file's doc for the operator-queue vs.
self-service-email design and the `docs/HOWTO.md` operator workflow.

## Per-IP + Per-Account Login Lockout

`NewLoginApi`'s `guard *sharedapis.LoginGuard` (built by `apps/myidsan/app/app.go`'s
`loginGuardConfig` from the shared `LoginSecurity` config block — the same one
`mymatasan`/`myiotsan` use, now optionally spanning a clustered deployment via
`WithSharedStore` — see `domain/shared/apis/login_guard_shared.go.md`) is applied to **every**
interactive *credential-guessing*
surface in this file: `defaultLogin` and `ldapLogin` both check `guardLocked` before
doing any credential work and respond `429` + `Retry-After` (`writeLoginLockout`) when
locked; only a genuine credential failure (`services.ErrInvalidCredential` /
`login.ErrLdapInvalidCredential`, never a payload or server error) sleeps the
configured `FailedDelay` and calls `guard.RecordFailure` (`recordLoginFailure`); a
success calls `guard.RecordSuccess` (`guardSuccess`). `writeLoginLockout` is a thin wrapper
over `writeLockout(w, retry, message)`, which carries the message as a parameter because the
SPA surfaces it verbatim and `apis/stepup.go` needs different wording for the same throttle
(see `apis/stepup.go.md`). **This closes a real gap**:
before this, myidsan had no failed-login lockout at all on either credential
surface. `apps/myidsan/apis/federated_auth.go`'s server-rendered `loginPost` shares
the identical guard instance, so the counters are shared across both login
surfaces, not per surface. `kerberosLogin` deliberately does **not** consult the
guard: a Kerberos ticket is cryptographically verified against the keytab (there is
no password to guess, and a forged/expired token is rejected by `AcceptSecContext`
itself), so per-IP credential-attempt throttling does not apply the same way it does
to a password or LDAP bind attempt.

**Two keys, not one (Productization Phase 3).** `loginGuardKey(r)` is the per-source
key (`RemoteAddr`'s host — never a spoofable forwarded header), throttling one machine
trying many accounts. `loginGuardAccountKey(identifier)` is the new per-account key
(`"user:" + lowercased identifier`, empty for an empty identifier); `loginGuardKeys(r,
identifier)` returns both (skipping the account key when empty) and is what
`guardLocked`/`guardSuccess`/`RecordFailure` now key against. Without the account key,
a password spray distributed across many source addresses against **one** account —
the shape credential-stuffing actually takes — was completely unthrottled no matter
how many attempts it made. `defaultLogin` and `ldapLogin` moved their `guardLocked`
check to **after** request-body decoding specifically so the attempted username is
available to key on. **Deliberate tradeoff**: an attacker who knows a username can now
lock that account out by spraying it — a nuisance the user recovers from by waiting
(and the lockout also clears on any successful sign-in, source and account both), set
against unlimited unthrottled guessing against a known account, which is not
recoverable. Token-based steps stay source-keyed only, since they present a challenge
token rather than an unproven username: `mfaLogin` (the second-factor redemption) and
`forgotPassword` (a recovery request never proves anything about the identifier it
names — throttling it per-account would let anyone lock a known user out of recovery
by repeatedly asking for it) both call `guardLocked`/`guardSuccess` with an empty
identifier.

## Federated Login Metrics

`MetricFederatedLoginTotal = "myidsan_federated_login_total"` counts LDAP **and**
Kerberos login outcomes by `{provider, result}` (`recordFederatedLogin`): LDAP via
`ldapResultLabel` (`success`, `invalid_credential`, `unreachable`, `ambiguous`,
`no_email`, `identity_conflict`, `disabled`, `inactive`, `error`); Kerberos via
`kerberosResultLabel` (`success`, `ticket_rejected`, `realm_refused`, `unreachable`,
`not_in_directory`, `identity_conflict`, `inactive`, `error` — see "Kerberos SPNEGO
SSO" above). LDAP's and Kerberos's failure modes (an unreachable directory, a wrong
SPN, clock skew) otherwise only ever surface as individual users failing to sign in,
with nothing in aggregate to alert an operator — this is also the label an operator
should correlate against the failure-modes table in `docs/HOWTO.md`'s Kerberos SSO
section when diagnosing a rollout.

## Federated Callback Flow (`providerCallback`)

Every rejection below is now audited via `recordFederatedLoginFailure` (above) — a refused
federated sign-in is exactly as security-relevant as a refused password, and these are the
events that expose a tampered callback, an IdP that stopped vouching for an address, or an
account someone disabled.

1. Resolve the provider from the registry; 404 if the key is unknown.
2. `provider.Callback(r)` exchanges the code and returns a normalized `login.Identity`; a
   provider error is audited (`attempted` empty — no identity resolved yet) and becomes
   `ErrStatusUnprocessableEntity`.
3. An identity with no email (e.g. a GitHub account with no public email) is audited
   (`attempted` = `identity.Subject`, the only available attribution) and rejected with
   `ErrStatusUnprocessableEntity` before it ever reaches the user service — no email means
   no account to show and no valid `Email` claim to issue.
4. `m.admitRedirectIdentity(r, identity)` resolves the identity to a local account:
   through `services.IDirectoryService.AdmitExternalIdentity` (see
   `services/directory.go.md`) when a directory service is wired, so a
   provider-scoped OIDC `groups` claim can seed a role for a still-pending account;
   falls back to the bare `m.userService.UpsertFederated(ctx, *identity)` otherwise
   (tests, minimal wiring) — Google/GitHub identities carry no `Groups` so the
   directory path is a no-op tail for them regardless. An error here is audited
   (`attempted` = `identity.Email`) before being mapped to a response:
   `ErrFederatedIdentityConflict`/`ErrInactiveAccount` → `ErrLimitedAccess`;
   `ErrFederatedIdentityInvalid` → `ErrStatusUnprocessableEntity`; anything else →
   `ErrInternalServerError`.
5. `setOAuthSession` issues the session cookies from the resolved `*entities.UserLogin` and
   the `*login.Identity` (display name falls back from `identity.Name` to the stored user's
   first/last name, then to the account email). A failure here is audited too
   (`"session issue failed: " + err.Error()`) before `ErrInternalServerError`.
6. Redirect to the `continue` target consumed via `consumeOAuthContinue`.

None of the four rejection paths advance the `LoginGuard` failed-login lockout (see "Per-IP
+ Per-Account Login Lockout" below) — deliberately: the credential was checked at the IdP,
not guessed here.

## Local Auth Contract

- Request login body: `username`, `password`.
- Request register body: `username`, `password`, optional `firstName`, `lastName`.
- `username` maps to `user_login.email`.
- Successful login/register responses return `{ result: { ok: true } }` and set the auth/CSRF cookies.
- Logout is available at `POST /api/login/default/logout` and clears both secure and local-development cookie names.

## Self-Throttled Password Re-checks (`selfThrottleLocked`/`selfThrottleFailure`)

Several *authenticated* endpoints re-check the signed-in caller's own password, which makes
each of them a password oracle for whoever holds the session cookie — they need no separate
credential of their own, just the ability to try candidates as fast as the network allows.
Two shared helpers, defined here and reused by `apis/mfa.go`'s `disable` and
`apis/webauthn.go`'s `remove` (both gained a `guard *sharedapis.LoginGuard` field/constructor
parameter for this), count these checks against the same lockout the login door uses — but
keyed on the SESSION's own account (`claims.Email`), never a submitted identifier, so unlike
the login door this counter cannot be aimed at somebody else by a stranger:

- `selfThrottleLocked(w, r, guard, email) bool` — checked first, before any credential work;
  answers `429` + `Retry-After` when the account is already locked and reports that it wrote
  the response.
- `selfThrottleFailure(guard, r, email)` — called on a wrong-password result; sleeps the
  configured `FailedDelay` (the delay matters as much as the counter — without it, every
  attempt before the threshold is a free guess) and calls `guard.RecordFailure`.

Covers `POST /api/login/default/change-password` (see below — the worst of the three, since a
correct guess here REPLACES the password and takes the account permanently),
`DELETE /api/mfa/webauthn/{id}` (`apis/webauthn.go.md`'s `remove`, which reproves identity
BEFORE looking up the key, so any key id works and no second factor is needed), and
`DELETE /api/mfa` (`apis/mfa.go.md`'s `disable`, defence-in-depth only — the valid-code gate
fires first there, so it was not actually reachable as an oracle before this either; throttled
anyway rather than relying on that ordering never changing).

## Change Password

`POST /api/login/default/change-password` is an authenticated endpoint (JWT cookie required). It verifies the caller's current password, hashes and stores the new one, and clears the `must_change_password` flag so the forced first-login gate is released. Returns `{ ok: true }` on success. Error responses distinguish between an incorrect current password (`ErrAuthFailed`) and a third-party-only account with no local password (`ErrLimitedAccess`). The new password must pass the configured password policy (`services.ValidatePassword`, `GET /api/login/password-policy` above publishes the rules; previously a hard-coded "at least 8 characters" — see `services/password_policy.go.md`).

Now checks `selfThrottleLocked` before any credential work, and on `ErrInvalidCredential` calls
`selfThrottleFailure` before responding — see "Self-Throttled Password Re-checks" above. On
success, calls `guardSuccess` to clear the caller's own counters (their own fat-fingering may
have built some up).

## Notes

- Federated providers are optional; local credential auth remains available even with an empty registry.
- Provider login start generates per-request state and stores it in an HTTP-only callback cookie (`setOAuthContinue`/provider's own `Login`).
- Provider callbacks validate the returned state before exchanging the provider code.
- Third-party accounts (empty password) are rejected for local credential login/register override.
- Federated login now carries the pending `continue` path (e.g. an `/api/auth/authorize` URL from a relying-app SSO redirect) through the round-trip. `setOAuthContinue` stores a base64-encoded, validated path in a short-lived provider-scoped HttpOnly cookie before the provider redirect; `consumeOAuthContinue` reads and clears it in the callback. `setOAuthSession` no longer writes a response body — control passes to the caller, which performs the redirect to the consumed `continue` target (or `/` when absent). This means a user arriving at myidsan's federated login page via a relying-app SSO redirect lands back at the relying app after completing federated login, not on a raw JSON payload.
- Account resolution for a federated login is centralized in `services.UpsertFederated` (strict `(provider, subject)` matching; a same-email account with no bound identity may claim it once, a bound one is refused) — this file no longer does its own `GetByEmail`-then-`Create` per provider, which is what let Google and GitHub each hand-roll a slightly different account-matching path before. `admitRedirectIdentity` wraps that same call with the directory's group→role seeding when a directory service is wired (see "Federated Callback Flow" above and `services/directory.go.md`); the underlying `(provider, subject)` matching is unchanged either way.
