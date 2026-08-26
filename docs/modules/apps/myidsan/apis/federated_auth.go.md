# Module: apps/myidsan/apis/federated_auth.go

## Purpose

Implements MyIDSan browser-facing authorization-code login for relying apps.

## Routes

- `GET /api/auth/authorize`: validates client, audience, redirect URI, and MyIDSan session, then redirects back with a one-time code.
- `GET /api/auth/login`: serves the MyIDSan login form (with optional social-login buttons, a Kerberos SSO button when configured, and, when directory login is enabled, an account-type select) when authorization starts without a MyIDSan session, via `renderLoginPage` (see below). The `continue` query parameter is carried through the page so a successful social/local/LDAP/Kerberos login redirects back to the authorization step. A failed Kerberos attempt (`apps/myidsan/apis/login.go`'s `kerberosLogin` redirecting to `?error=sso_failed`) is handled here too: `loginPage` renders an inline "Single sign-on failed..." message instead of the browser landing on a bare `401`.
- `POST /api/auth/login`: authenticates local **or** LDAP credentials via `loginPost` (branching on the posted `method` field). If the resolved account has a confirmed second factor, withholds the session and renders the MFA challenge instead (see "Second-Factor (MFA) Login Challenge" below); otherwise issues the MyIDSan session cookie and resumes authorization. On a credential failure, re-renders the same branded card (`renderLoginPage` with `errMsg` set, `username` echoed back, and the account-type selection preserved) instead of returning bare unstyled text — the user only has to retype the password.
- `POST /api/auth/mfa`: server-rendered second-factor step (`mfaPost`) — redeems the challenge token + code posted from `renderMfaChallenge` and, on success, issues the session `loginPost` withheld.
- `GET /api/auth/forgot` / `POST /api/auth/forgot`: server-rendered account-recovery request form (`forgotPage`/`forgotPost`) — see "Account Recovery (Forgot Password)" below.
- `GET /api/auth/reset` / `POST /api/auth/reset`: server-rendered set-new-password form for a valid self-service email token (`resetPage`/`resetPost`) — only reachable when the optional SMTP relay is configured.
- `POST /api/auth/token`: exchanges one authorization code for a signed relying-app token response.

## Second-Factor (MFA) Login Challenge

`NewFederatedAuthApi` now also takes `mfaService services.IMfaService` and
`webauthnService services.IWebAuthnService` parameters (either may be `nil` — tests and
minimal wiring pass `nil` explicitly, see `federated_auth_test.go`'s calls); when `store`
(the existing `cache.Store` parameter) is non-nil and at least one of them is set, the
handler builds the same kind of `*mfaChallenger` that `apis/login.go` builds (see
`apis/mfa_challenge.go.md`) — a separate instance, but backed by the same underlying
`cache.Store`/`IMfaService`/`IWebAuthnService`, so a challenge token issued by one login
surface can, in principle, be redeemed by the other.

**Security fix, found and closed alongside adding security keys: `webauthnService` was
initially left out of this constructor call entirely.** This page is where a relying
app's SSO hop lands (`myseliasan` included), so an account whose *only* enrolled second
factor is a security key would have had `mfaChallenger.required` answer `false` — no
TOTP factor, and the challenger did not yet know to ask about WebAuthn — and would have
been handed a session with **no** second factor checked at all, on every relying app in
the suite. Fixed by threading `webauthnService` through from `apps/myidsan/app/app.go`
the same way `apis/login.go` does; see that file's "Second-Factor Login: Security Keys"
section for the twin PUBLIC JSON endpoints this page's inline JS calls.

- `loginPost`, after a successful local/LDAP credential check and *before* calling
  `issueProviderSession`, checks `m.mfa.required`. If the account owes a second factor
  (TOTP confirmed, a security key enrolled, or both), it mints a challenge token
  (`m.mfa.issue`) and calls `renderMfaChallenge` instead of setting any cookie or
  redirecting — the same pre-session ordering the SPA uses. No `kopiv2_access` cookie
  exists until a factor is verified.
- `renderMfaChallenge(w, r, status, continueTo, token, errMsg)` resolves which factor
  kinds this specific account can present — `m.mfa.peek(ctx, r, token)` (re-checks the
  client fingerprint, does **not** spend the token) followed by `m.mfa.methods` — and
  renders accordingly:
  - The TOTP code form renders only when `"totp"` is offered (or when `methods` came
    back empty, e.g. an older-shaped challenger — the code form is the fall-back), so a
    key-only account is never shown a field it can never fill.
  - A **"Use a security key"** block renders only when `"webauthn"` is offered: a
    button plus inline vanilla JS (`navigator.credentials.get`, base64url ↔
    `ArrayBuffer` helpers hand-rolled here rather than imported, since this page has no
    build step) that calls the exact same public JSON endpoints the SPA uses —
    `POST /api/login/mfa/webauthn/{begin,finish}` — and on success navigates to
    `continueTo` itself. Built with a `strings.Replacer` (`jsString`), never `Sprintf`,
    for the embedded script: a `Sprintf` here would still format-scan every `%` the
    inline JavaScript's base64 padding math needs, and getting that escaping wrong
    would emit a syntax error into the page — reachable only in the browser, and only
    for a key-only account, so easy to ship unnoticed.
  - Both blocks can render together when an account holds both factor kinds, giving the
    user a choice; the page never claims a factor exists that the account does not
    actually hold.
- `mfaPost` (the TOTP form's target — the WebAuthn block posts to the JSON endpoints
  above instead, not to this handler) parses the form, validates the CSRF double-submit
  token (re-rendering the challenge with "that form expired" on a mismatch), checks
  `guardLocked` — source-IP key only, since this step presents a challenge token rather
  than a username (rendering the challenge again with a `429` "too many failed
  attempts" message if locked) — then calls `m.mfa.redeem(ctx, r, token, code)`, which now
  also returns `usedRecovery`:
  - `services.ErrMfaBadCode` → sleeps the guard's failure delay, records the
    failure, and re-renders the challenge (`401`) with "That code did not match" —
    the token survives so the user can retry until it hits its attempt cap or TTL. Also
    records `services.ActionMfaChallenge` / `OutcomeDenied` via the new `recordAuth`
    helper (below) — under its own action rather than `login.failure`, since whoever is
    grinding codes here has already cleared the password, a different and later incident.
  - Any other error (expired/exhausted/rebound token) → redirects to
    `/api/auth/login?error=sso_failed` (preserving `continue`) — the whole login
    restarts, mirroring a failed Kerberos attempt.
  - Success records the guard success, reloads the account (`loadActiveUserById`,
    re-checking `IsActive`); if `usedRecovery` is true, also records
    `services.ActionMfaRecovery` (`Metadata: {method: recovery_code, surface: "browser"}`,
    via `recordAuth`) — the browser-leg twin of `login.go.md`'s `recordRecoveryLogin`, since
    without it a break-glass sign-in on this surface looks exactly like any other — then
    calls `issueProviderSession` + redirects to `continueTo` — the session `loginPost`
    withheld.
- `continueQuery(continueTo)` is a small helper that renders `&continue=<escaped>`
  for the `sso_failed` redirect, omitted for the root path.
- `jsString(value string) string` renders a Go string as a JSON literal safe to embed in
  an inline `<script>` — `json.Marshal` plus `<`/`>`/`&` escaping, since a literal
  `</script>` inside an embedded JSON string would otherwise end the script block early.

## Account Recovery (Forgot Password)

`NewFederatedAuthApi` now also takes a `resetService services.IPasswordResetService`
trailing parameter (may be `nil` — tests pass `nil`, see `federated_auth_test.go`),
stored as `m.reset`. This is the server-rendered half of account recovery — the two
channels are: an always-on operator queue (superadmin resolves a request by issuing a
temporary password out-of-band, see `apis/password_reset.go.md`) and an **optional**
SMTP self-service email link, active only when `config.smtp.enabled` (see
`infra/mailer/mailer.go.md`, `services/password_reset.go.md`). Only **local**
accounts are eligible; LDAP/Kerberos/OIDC accounts have no password here to reset and
are silently excluded by the service layer (no oracle either way).

- `authPageShell(subtitle, innerHTML)` wraps the recovery/reset card content in the
  same branded, self-hosted-assets-only chrome `renderLoginPage`/`renderMfaChallenge`
  use, without duplicating the full stylesheet per handler.
- The login page (`renderLoginPage`) now carries a "Forgot your password?" link to
  `/api/auth/forgot`.
- `forgotPage` renders the request form (username/email). `forgotPost` validates the
  CSRF double-submit token (see "CSRF on the Server-Rendered Auth Forms" below;
  re-renders "that form expired" on a mismatch), then checks `guardLocked` — here keyed
  on **both** the source IP and the submitted username (`guardLocked(m.guard, r,
  r.Form.Get("username"))`) — before calling `m.reset.Request(ctx, username,
  loginGuardKey(r), requestOrigin(r))`. **Note:** this differs from the SPA's
  equivalent JSON endpoint, `apis/login.go`'s `forgotPassword`, which deliberately
  checks the guard *before* parsing the body so only the source-IP key is ever used —
  that file's comment argues that keying a recovery-request check on the submitted
  account would let anyone lock a known user out of recovery simply by repeatedly
  naming them. This page's account-keyed check was not called out as an intentional
  deviation; flagged here as worth reconciling. Either way, the response **always**
  renders the same generic confirmation — wording only varies on
  `m.reset.MailEnabled()` ("an administrator has been notified" vs. "we've emailed a
  link... valid for 30 minutes") — never on whether an account actually matched.
- `resetPage` (`GET /api/auth/reset?token=...`) is only meaningful when the SMTP link
  was used: it calls `m.reset.ResolveToken` to validate the token before rendering the
  set-new-password form; an invalid/expired/missing token (or `m.reset == nil`) renders
  an error card with a link back to `/api/auth/forgot` instead. The form's copy and the
  `<input>` no longer hard-code "at least 8 characters"/`minlength="8"` — the password
  policy is now configurable (`services/password_policy.go.md`), so the hint reads
  "must meet this server's password policy" and client-side length is unenforced,
  relying entirely on the server-side `ValidatePassword` check in `resetPost`.
- `resetPost` validates the CSRF double-submit token (see below; a mismatch renders
  "that form expired, open the link from your email again") before doing anything else
  — without it, a victim holding a valid reset token could be made to submit a password
  of the attacker's choosing from their own browser. It then calls
  `m.reset.CompleteSelfService(ctx, token, password)`, which sets the
  new password and consumes the token (single-use). On success it renders a plain
  "password has been reset, you can now sign in" confirmation — **deliberately does
  NOT issue a session**: any second factor the account has enrolled is still presented
  at the next normal login, exactly as if the user had typed their old password.
- On `resetPost` failure, `services.ErrResetTokenInvalid` renders "link is invalid or
  has expired" (with a link back to `/api/auth/forgot`); any other error (e.g. the
  service layer's password-length check) is surfaced verbatim as "Could not set the
  password: ..." — there is no oracle risk once the token itself has already proven
  possession of the account.

## `renderLoginPage`

Draws the federated sign-in page — the suite's one server-rendered login screen, since
this page sits in the middle of the OAuth redirect chain and must exist before any SPA
loads. It reuses the same visual language as mymatasan's/myseliasan's React
`.login-screen`/`.login-panel` chrome (light-theme color literals, since there is no
stylesheet to import CSS custom properties from here), self-hosted assets only
(`/assets/favicon.svg`, `/assets/fonts.css` — no external font/CDN references, required
because myidsan and the apps that redirect here run on an air-gapped intranet), and
takes `(w, r, status, continueTo, username, method, ldapLabel, errMsg)` — `r` was added
(Productization Phase 3) so the CSRF token can be minted per render; see "CSRF on the
Server-Rendered Auth Forms" below. `status`/`errMsg`
let `loginPost` re-render the card with an inline error on `POST` failure instead of the
page always being a plain `GET` 200; `username` is echoed back into the form on a
failed attempt; `method` (`"local"`/`"ldap"`) preserves the account-type choice across
a failed POST; `ldapLabel` non-empty renders an "Account type" `<select>` (Local
account / `ldapLabel`) — empty (directory login disabled, via `directoryLabel(r)`,
which asks `services.IDirectoryService.LoginOption`) renders no select at all, so a
disabled directory never offers a dead choice.

## `loginPost` — local vs. LDAP

`loginPost` validates the CSRF double-submit token first (see "CSRF on the
Server-Rendered Auth Forms" below); a mismatch re-renders the login page with "that
form expired, please try again" (`400`) rather than an error — a stale/back-navigated
tab is the common legitimate cause, and the fresh render carries a fresh token.
`useLdap := method == "ldap" && ldapLabel != ""` — an "ldap" POST against a currently
disabled directory silently falls back to a local credential check (`method` is reset
to `"local"`) rather than erroring, since the account-type choice only exists in the
rendered form while directory login is enabled. The shared `*sharedapis.LoginGuard`
(`m.guard`, the same instance `apis/login.go` uses — see that doc's "Per-IP + Per-Account
Login Lockout" section) is checked (keyed on both source IP and the posted `username`)
before either credential path runs, and a genuine credential failure
(`services.ErrInvalidCredential` or `login.ErrLdapInvalidCredential`) records against
both guard counters after the configured failure delay.

## CSRF on the Server-Rendered Auth Forms

`/api/auth/{login,mfa,forgot,reset}` are public routes that exist precisely for callers
with no session yet, so they never pass through the auth middleware's session-bound CSRF
check and previously had none at all — a login-CSRF/session-fixation gap, and a way to
force a password-reset submission on behalf of a victim holding a valid reset token. Every
render (`renderLoginPage`, `renderMfaChallenge`, `forgotPage`, `resetPage`) now mints a
session-less double-submit token via `issueAuthFormCSRF`/`authFormCSRFInput`
(`apps/myidsan/apis/auth_form_csrf.go`, see that file's doc) and embeds it as a hidden
field; every POST handler (`loginPost`, `mfaPost`, `forgotPost`, `resetPost`) calls
`validateAuthFormCSRF(r)` first and re-renders the originating form on a mismatch.
**Gotcha, worth knowing before touching any of these renderers:** the token must be minted
into a local variable *before* `w.WriteHeader(status)` — `http.SetCookie` only appends to
the response's header map, and a cookie set from inside the `fmt.Fprintf` argument list
(after the status line has already been written) is silently discarded, leaving a form
whose token has no matching cookie and every genuine submission failing. See
`apps/myidsan/apis/auth_form_csrf.go.md`. In `renderMfaChallenge`, the mint now happens
inside `codeFormHTML`'s construction (still before `WriteHeader`) and is skipped
entirely for a key-only account — the TOTP form is the only thing on this page that
posts back with a hidden CSRF field; the WebAuthn block's inline JS calls the public
JSON ceremony endpoints directly and needs no CSRF token of its own.

## `socialButtonsHTML`

Renders one button per provider in the shared `*login.Registry` (`m.providers`, built
once by `NewLoginApi` and passed into `NewFederatedAuthApi`) — the same registry that
drives the SPA's `/api/login/providers` list and the `/api/login/{provider}` routes, so
this page can never offer a button the routing layer would then 404. The registry only
holds providers whose credentials are actually present (`login.BuildRegistry`'s
`ClientId`/`ClientSecret`-non-empty guard): the stock `config.json` ships empty
`google`/`github` objects, and a non-nil-but-blank provider used to render a button that
sent the browser to `accounts.google.com` with an empty `client_id` (an OAuth error on
the internet, and a dead end on an intranet where myidsan is deployed — myseliasan's SSO
hop lands on this exact page). When `m.kerbLabel` is non-empty (set from the
`kerberosLabel` constructor parameter, itself sourced from
`deps.Config.Kerberos.DisplayLabel`) an extra button links to
`/api/login/kerberos?continue=...` — plain navigation, since the `401`/Negotiate dance
happens transparently in the browser; a failure bounces back to this same page with
`error=sso_failed`. The row is rendered as soon as *either* the registry is non-empty
or `m.kerbLabel` is set (an empty registry with Kerberos configured must still show its
button). Covered by `federated_auth_login_test.go`:
`TestSocialButtonsRequireCredentials` (buttons appear only once both fields are set,
now exercised through `login.BuildRegistry`) and `TestLoginPageHasNoExternalReferences`
(the rendered page references no external host and only same-origin asset paths).

## Security

- Client registration is loaded from `app_auth_config`.
- Callback URLs must match active `app_redirect_uri` rows exactly.
- Authorization codes are random, short-lived, stored in cache, and deleted after token exchange.
- Client secrets are verified with `secretMatches`, which now hashes with **bcrypt**
  (Productization Phase 3) rather than the previous unsalted single-round SHA-256 — a
  read of `app_auth_config` used to hand an attacker every operator-chosen client
  secret at GPU speed. `secretMatches` accepts **both** the current bcrypt form and the
  legacy SHA-256 form (`isBcryptClientSecret` distinguishes them by the `$2a$`/`$2b$`/
  `$2y$` prefix) and reports `needsRehash`; `token` (`POST /api/auth/token`) rewrites a
  legacy row to bcrypt the first time its secret is presented (best-effort — a rewrite
  failure is logged, never fails the token exchange that already succeeded), so an
  existing install migrates itself without an operator re-entering every client
  secret. `hashClientSecret` (used by `apps/myidsan/apis/app_auth_config.go`'s create/
  update handlers) now returns `(string, error)` for the same reason.
- Login resume paths (`cleanContinuePath`, guarding the OAuth `continue` cookie and the Kerberos failure redirect) reject absolute external URLs, any value carrying a host (`parsed.Host != ""`, catching `//evil.example` even though it isn't scheme-absolute), and any value containing a backslash. The backslash check is load-bearing on its own: browsers normalise `\` to `/` in the authority position, so `/\evil.example` used to pass both the `IsAbs()` check and the `strings.HasPrefix(value, "//")` check and then navigate off-origin as a protocol-relative URL — an open redirect on the login flow, the classic phishing primitive against an identity provider. Covered by `TestCleanContinuePathRejectsBackslashBypass` and `TestCleanContinuePathRejectsEmbeddedHost` in `federated_auth_test.go`.
- The rendered login page loads no external host (no CDN, no Google Fonts) — every asset is a same-origin path, verified by `federated_auth_login_test.go`.
- `NewFederatedAuthApi` now also takes `directory services.IDirectoryService`, `kerberosLabel string`, `guard *sharedapis.LoginGuard`, `mfaService services.IMfaService`, `resetService services.IPasswordResetService`, `metrics telemetry.Metrics`, and a trailing `webauthnService services.IWebAuthnService` (directory/guard/mfaService/resetService/metrics/webauthnService may be nil — directory login off / lockout off / TOTP not armed / recovery pages a no-op / token-exchange outcomes not counted / security keys not offered on this page; `kerberosLabel` empty means Kerberos is not offered on this page); LDAP credential checks go through `directory.AuthenticateLdap`, never a hand-rolled bind here. Kerberos itself is never invoked from this file — SPNEGO verification only ever happens on `apps/myidsan/apis/login.go`'s dedicated `GET /api/login/kerberos` route; this file only decides whether to render the button and whether to show the `sso_failed` inline error.
- `token` (`POST /api/auth/token`) records `MetricTokenExchangeTotal{outcome}` on every
  path out of the handler via the `recordTokenExchange` helper (nil-safe when `metrics` is
  nil) — bad request, unsupported grant type, unknown client, invalid secret, invalid
  redirect URI, a code that's missing/expired/already redeemed, a code that doesn't match
  the presented client, a signing failure, and success. This is the only place a failed
  redemption is visible to myidsan itself: the caller is a relying app, which shows its own
  user its own error, so without the counter a client whose secret was rotated (or whose
  redirect URI drifted) fails silently from myidsan's side. See
  `services/metrics.go.md` for the outcome constants and rationale.
- Kerberos does not gate on MFA either — same rationale as the SPA login path (upstream IdP owns factor policy, see `docs/MYIDSAN_MFA_PLAN.md` §5).
- The self-service password-reset flow never issues a session and never distinguishes "unknown account" from "account matched, email sent/admin notified" in any response — see "Account Recovery (Forgot Password)" above.

## The federation trail

**What was missing, and it was the whole of the interesting half.** The trail recorded that an
account signed in (`login.success`, from the credential step). It recorded nothing about what
that sign-in OPENED. On an identity server those are different facts: the credential check
happens once, and the access it is traded for happens **per relying app**. So
"this account was compromised — which applications did it reach?" was a question only myidsan
could answer, and the one party that did not write it down. Every relying app simply saw a
federated session appear.

Three actions now cover it (`services/audit.go`):

| Action | Written when |
|---|---|
| `sso.authorize` | An authorization code is issued for a client. |
| `sso.token_issue` | That code is exchanged for an access token. |
| `sso.refused` | An unknown client, an unregistered redirect URI, a bad audience, or a code that is expired, unknown or **already used**. |

Every entry names the **client and the account together**, because neither half answers the
question alone.

**The refusals are the more useful half.** An unregistered redirect URI is what an attempt to
have somebody's authorization code delivered elsewhere looks like; a replayed code is what
using a stolen one looks like. A trail holding only successes cannot show either.

`recordSso` is nil-safe and never fails the request: an identity server that refuses to sign
somebody in because it could not write a log line has turned an observability problem into an
outage.

`recordAuth(r, e services.AuditEntry)` is the newer, narrower sibling used by `mfaPost`
(above) for the two second-factor events (a refused code, a recovery-code burn): separate
from `recordSso` because those entries are **federation** events targeting a client id,
whereas these target the account and would be unreadable filed under an `sso_client` field
with no client to name. Same nil-safety and client-context resolution as `recordSso`.

**Why the unit tests did not catch this.** `login_federated_audit_test.go` exists and passes —
it covers the credential step, which was always audited. Nothing crossed the boundary between
the two apps, which is where the gap was. Found by `tools/fleetbench/bench_idsan_sso.py`, the
first live exercise this app has ever had.

## Session indexing (`issueProviderSession`)

This page is where a relying app's SSO hop lands, so nearly every real session in the estate is
issued here — and until a live bench went looking, **none of them were indexed**. `issueProviderSession`
called `IssueAuthCookies` and returned, so the session had no `user_session` row: it could not be
listed by its owner, could not be seen by an administrator, and could not be revoked. Revoking
answered `{"ok":true,"revoked":0}` — success, having done nothing.

It now pre-mints the session id and indexes it through the shared helper in
`apps/myidsan/apis/session_index.go.md`. The constructor gained a `sessions services.ISessionService`
parameter for it; passing nil leaves the behaviour exactly as it was, which is what the tests do.

## `POST /api/auth/session-status`

Mounted on this router (`sessionStatus`, implemented in `session_status.go`): how a relying app
learns that a session it is still serving has been revoked at this server. Authenticated with the
same `client_id`/`client_secret` pair `/api/auth/token` accepts. See
`apps/myidsan/apis/session_status.go.md`.
