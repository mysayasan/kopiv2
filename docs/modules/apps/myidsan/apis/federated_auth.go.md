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

`NewFederatedAuthApi` now also takes an `mfaService services.IMfaService` parameter
(may be `nil` — tests and minimal wiring pass `nil` explicitly, see
`federated_auth_test.go`'s calls); when both it and `store` (the existing `cache.Store`
parameter) are non-nil, the handler builds the same kind of `*mfaChallenger` that
`apis/login.go` builds (see `apis/mfa_challenge.go.md`) — a separate instance, but
backed by the same underlying `cache.Store` and `IMfaService`, so a challenge token
issued by one login surface can, in principle, be redeemed by the other.

- `loginPost`, after a successful local/LDAP credential check and *before* calling
  `issueProviderSession`, checks `m.mfa.required`. If the account has a confirmed
  factor, it mints a challenge token (`m.mfa.issue`) and calls `renderMfaChallenge`
  instead of setting any cookie or redirecting — the same pre-session ordering the
  SPA uses. No `kopiv2_access` cookie exists until the code is verified.
- `renderMfaChallenge(w, status, continueTo, token, errMsg)` draws a minimal
  single-field code form (reusing the login card chrome, self-hosted assets only)
  posting back to `/api/auth/mfa` with the opaque token and the pending `continue`
  target as hidden fields.
- `mfaPost` parses the form, checks `guardLocked` (rendering the challenge again
  with a `429` "too many failed attempts" message if locked), then calls
  `m.mfa.redeem(ctx, r, token, code)`:
  - `services.ErrMfaBadCode` → sleeps the guard's failure delay, records the
    failure, and re-renders the challenge (`401`) with "That code did not match" —
    the token survives so the user can retry until it hits its attempt cap or TTL.
  - Any other error (expired/exhausted/rebound token) → redirects to
    `/api/auth/login?error=sso_failed` (preserving `continue`) — the whole login
    restarts, mirroring a failed Kerberos attempt.
  - Success records the guard success, reloads the account (`loadActiveUserById`,
    re-checking `IsActive`), and calls `issueProviderSession` + redirects to
    `continueTo` — the session `loginPost` withheld.
- `continueQuery(continueTo)` is a small helper that renders `&continue=<escaped>`
  for the `sso_failed` redirect, omitted for the root path.

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
- `forgotPage` renders the request form (username/email). `forgotPost` checks
  `guardLocked` (the same shared `LoginGuard` credential surfaces use, so the
  endpoint cannot be hammered to fingerprint accounts), then calls
  `m.reset.Request(ctx, username, loginGuardKey(r), requestOrigin(r))` and **always**
  renders the same generic confirmation — wording only varies on `m.reset.MailEnabled()`
  ("an administrator has been notified" vs. "we've emailed a link... valid for 30
  minutes") — never on whether an account actually matched.
- `resetPage` (`GET /api/auth/reset?token=...`) is only meaningful when the SMTP link
  was used: it calls `m.reset.ResolveToken` to validate the token before rendering the
  set-new-password form; an invalid/expired/missing token (or `m.reset == nil`) renders
  an error card with a link back to `/api/auth/forgot` instead.
- `resetPost` calls `m.reset.CompleteSelfService(ctx, token, password)`, which sets the
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
takes `(w, status, continueTo, username, method, ldapLabel, errMsg)`: `status`/`errMsg`
let `loginPost` re-render the card with an inline error on `POST` failure instead of the
page always being a plain `GET` 200; `username` is echoed back into the form on a
failed attempt; `method` (`"local"`/`"ldap"`) preserves the account-type choice across
a failed POST; `ldapLabel` non-empty renders an "Account type" `<select>` (Local
account / `ldapLabel`) — empty (directory login disabled, via `directoryLabel(r)`,
which asks `services.IDirectoryService.LoginOption`) renders no select at all, so a
disabled directory never offers a dead choice.

## `loginPost` — local vs. LDAP

`useLdap := method == "ldap" && ldapLabel != ""` — an "ldap" POST against a currently
disabled directory silently falls back to a local credential check (`method` is reset
to `"local"`) rather than erroring, since the account-type choice only exists in the
rendered form while directory login is enabled. The shared `*sharedapis.LoginGuard`
(`m.guard`, the same instance `apis/login.go` uses — see that doc's "Per-IP Login
Lockout" section) is checked before either credential path runs, and a genuine
credential failure (`services.ErrInvalidCredential` or `login.ErrLdapInvalidCredential`)
records against the guard's per-IP counters after the configured failure delay.

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
- Client secrets are verified against stored SHA-256 hashes.
- Login resume paths reject absolute external URLs.
- The rendered login page loads no external host (no CDN, no Google Fonts) — every asset is a same-origin path, verified by `federated_auth_login_test.go`.
- `NewFederatedAuthApi` now also takes `directory services.IDirectoryService`, `kerberosLabel string`, `guard *sharedapis.LoginGuard`, `mfaService services.IMfaService`, and a trailing `resetService services.IPasswordResetService` (directory/guard/mfaService/resetService may be nil — directory login off / lockout off / MFA not armed / recovery pages a no-op; `kerberosLabel` empty means Kerberos is not offered on this page); LDAP credential checks go through `directory.AuthenticateLdap`, never a hand-rolled bind here. Kerberos itself is never invoked from this file — SPNEGO verification only ever happens on `apps/myidsan/apis/login.go`'s dedicated `GET /api/login/kerberos` route; this file only decides whether to render the button and whether to show the `sso_failed` inline error.
- Kerberos does not gate on MFA either — same rationale as the SPA login path (upstream IdP owns factor policy, see `docs/MYIDSAN_MFA_PLAN.md` §5).
- The self-service password-reset flow never issues a session and never distinguishes "unknown account" from "account matched, email sent/admin notified" in any response — see "Account Recovery (Forgot Password)" above.
