# Module: apps/myidsan/apis/federated_auth.go

## Purpose

Implements MyIDSan browser-facing authorization-code login for relying apps.

## Routes

- `GET /api/auth/authorize`: validates client, audience, redirect URI, and MyIDSan session, then redirects back with a one-time code.
- `GET /api/auth/login`: serves the MyIDSan login form (with optional social-login buttons and, when directory login is enabled, an account-type select) when authorization starts without a MyIDSan session, via `renderLoginPage` (see below). The `continue` query parameter is carried through the page so a successful social/local/LDAP login redirects back to the authorization step.
- `POST /api/auth/login`: authenticates local **or** LDAP credentials via `loginPost` (branching on the posted `method` field), issues the MyIDSan session cookie, and resumes authorization. On failure, re-renders the same branded card (`renderLoginPage` with `errMsg` set, `username` echoed back, and the account-type selection preserved) instead of returning bare unstyled text — the user only has to retype the password.
- `POST /api/auth/token`: exchanges one authorization code for a signed relying-app token response.

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
hop lands on this exact page). Covered by `federated_auth_login_test.go`:
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
- `NewFederatedAuthApi` now also takes `directory services.IDirectoryService` and `guard *sharedapis.LoginGuard` (both may be nil — directory login off / lockout off); LDAP credential checks go through `directory.AuthenticateLdap`, never a hand-rolled bind here.
