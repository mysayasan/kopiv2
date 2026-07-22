# Module: apps/myidsan/apis/login.go

## Purpose

Provides authentication endpoints for local credentials, LDAP/Active Directory
credentials, Kerberos SPNEGO SSO, and any registered redirect-style federated
identity provider (`infra/login.Registry`, see `infra/login/provider.go.md`).

## Responsibilities

- Handles local credential login via `POST /api/login/default`.
- Handles local account registration via `POST /api/login/default/register`.
- Handles LDAP/Active Directory credential login via `POST /api/login/ldap` (`ldapLogin`) — a JSON credential POST, not a browser redirect, so it is a fixed route rather than a member of the redirect-provider registry. Disabled (`directory == nil` or the configured directory is off) responds `ErrLimitedAccess`. On success it calls `services.IDirectoryService.AuthenticateLdap` (see `services/directory.go.md`) and issues the session through the same `issueLocalSession` local login uses.
- Handles Kerberos SPNEGO SSO via `GET /api/login/kerberos` (`kerberosLogin`) — see "Kerberos SPNEGO SSO" below.
- Sets HttpOnly JWT session cookies for successful local/LDAP/Kerberos login/register and federated callbacks.
- Sets a readable CSRF cookie that clients echo in `X-CSRF-Token` for unsafe authenticated requests.
- Clears session cookies through logout.
- `NewLoginApi` builds the provider registry via `login.BuildRegistry(oAuth2Conf)` (Google/GitHub register only when both `ClientId` and `ClientSecret` are present) and **returns it**, so `apps/myidsan/app/app.go` can thread the same registry into `NewFederatedAuthApi` — one registry, one provider list, shared by the server-rendered login page, the SPA, and the OAuth routes. `NewLoginApi`'s remaining parameters are now collected into a `LoginApiOptions` struct (`Directory services.IDirectoryService`, `Kerberos *login.KerberosAuthenticator`, `KerberosLabel string`, `Guard *sharedapis.LoginGuard`, `Metrics telemetry.Metrics`) — every field may be zero (tests pass the empty struct; LDAP/Kerberos disabled / lockout off / metrics not wired). `KerberosLabel` defaults to `"Windows (SSO)"` when blank and `Kerberos` is non-nil.
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
     `recordFederatedLogin`/`kerberosResultLabel`, and redirected via
     `redirectKerberosFailure`.
3. `resolveKerberosUser(r, principal)`: if a directory is configured, resolves
   through `services.IDirectoryService.ResolveDirectoryUser` (the directory
   describes the ticket-verified principal — see `services/directory.go.md`);
   `ErrDirectoryDisabled` falls back to
   `login.StandaloneKerberosIdentity(principal)` +
   `IUserLoginService.UpsertFederated` for directory-less installs. Any other
   resolution error (not found, identity conflict, inactive account) is also
   recorded and redirected.
4. Success calls the shared `issueSessionCookies` (below) and redirects
   `302` to the pending `continue` target.
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

## Per-IP Login Lockout

`NewLoginApi`'s `guard *sharedapis.LoginGuard` (built by `apps/myidsan/app/app.go`'s
`loginGuardConfig` from the shared `LoginSecurity` config block — the same one
`mymatasan`/`myiotsan` use) is applied to **every** interactive *credential-guessing*
surface in this file: `defaultLogin` and `ldapLogin` both check `guardLocked` before
doing any credential work and respond `429` + `Retry-After` (`writeLoginLockout`) when
locked; only a genuine credential failure (`services.ErrInvalidCredential` /
`login.ErrLdapInvalidCredential`, never a payload or server error) sleeps the
configured `FailedDelay` and calls `guard.RecordFailure` (`recordLoginFailure`); a
success calls `guard.RecordSuccess` (`guardSuccess`). `loginGuardKey` keys strictly on
`RemoteAddr`'s host — never a spoofable forwarded header. **This closes a real gap**:
before this change myidsan had no failed-login lockout at all on either credential
surface. `apps/myidsan/apis/federated_auth.go`'s server-rendered `loginPost` shares
the identical guard instance, so the counters are per source IP across both login
surfaces, not per surface. `kerberosLogin` deliberately does **not** consult the
guard: a Kerberos ticket is cryptographically verified against the keytab (there is
no password to guess, and a forged/expired token is rejected by `AcceptSecContext`
itself), so per-IP credential-attempt throttling does not apply the same way it does
to a password or LDAP bind attempt.

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

1. Resolve the provider from the registry; 404 if the key is unknown.
2. `provider.Callback(r)` exchanges the code and returns a normalized `login.Identity`; a provider error becomes `ErrStatusUnprocessableEntity`.
3. An identity with no email (e.g. a GitHub account with no public email) is rejected with `ErrStatusUnprocessableEntity` before it ever reaches the user service — no email means no account to show and no valid `Email` claim to issue.
4. `m.admitRedirectIdentity(r, identity)` resolves the identity to a local account:
   through `services.IDirectoryService.AdmitExternalIdentity` (see
   `services/directory.go.md`) when a directory service is wired, so a
   provider-scoped OIDC `groups` claim can seed a role for a still-pending account;
   falls back to the bare `m.userService.UpsertFederated(ctx, *identity)` otherwise
   (tests, minimal wiring) — Google/GitHub identities carry no `Groups` so the
   directory path is a no-op tail for them regardless. Errors map to responses:
   `ErrFederatedIdentityConflict`/`ErrInactiveAccount` → `ErrLimitedAccess`;
   `ErrFederatedIdentityInvalid` → `ErrStatusUnprocessableEntity`; anything else →
   `ErrInternalServerError`.
5. `setOAuthSession` issues the session cookies from the resolved `*entities.UserLogin` and the `*login.Identity` (display name falls back from `identity.Name` to the stored user's first/last name, then to the account email).
6. Redirect to the `continue` target consumed via `consumeOAuthContinue`.

## Local Auth Contract

- Request login body: `username`, `password`.
- Request register body: `username`, `password`, optional `firstName`, `lastName`.
- `username` maps to `user_login.email`.
- Successful login/register responses return `{ result: { ok: true } }` and set the auth/CSRF cookies.
- Logout is available at `POST /api/login/default/logout` and clears both secure and local-development cookie names.

## Change Password

`POST /api/login/default/change-password` is an authenticated endpoint (JWT cookie required). It verifies the caller's current password, hashes and stores the new one, and clears the `must_change_password` flag so the forced first-login gate is released. Returns `{ ok: true }` on success. Error responses distinguish between an incorrect current password (`ErrAuthFailed`) and a third-party-only account with no local password (`ErrLimitedAccess`). New password must be at least 8 characters (enforced by the service layer).

## Notes

- Federated providers are optional; local credential auth remains available even with an empty registry.
- Provider login start generates per-request state and stores it in an HTTP-only callback cookie (`setOAuthContinue`/provider's own `Login`).
- Provider callbacks validate the returned state before exchanging the provider code.
- Third-party accounts (empty password) are rejected for local credential login/register override.
- Federated login now carries the pending `continue` path (e.g. an `/api/auth/authorize` URL from a relying-app SSO redirect) through the round-trip. `setOAuthContinue` stores a base64-encoded, validated path in a short-lived provider-scoped HttpOnly cookie before the provider redirect; `consumeOAuthContinue` reads and clears it in the callback. `setOAuthSession` no longer writes a response body — control passes to the caller, which performs the redirect to the consumed `continue` target (or `/` when absent). This means a user arriving at myidsan's federated login page via a relying-app SSO redirect lands back at the relying app after completing federated login, not on a raw JSON payload.
- Account resolution for a federated login is centralized in `services.UpsertFederated` (strict `(provider, subject)` matching; a same-email account with no bound identity may claim it once, a bound one is refused) — this file no longer does its own `GetByEmail`-then-`Create` per provider, which is what let Google and GitHub each hand-roll a slightly different account-matching path before. `admitRedirectIdentity` wraps that same call with the directory's group→role seeding when a directory service is wired (see "Federated Callback Flow" above and `services/directory.go.md`); the underlying `(provider, subject)` matching is unchanged either way.
