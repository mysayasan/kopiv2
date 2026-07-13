# Module: apps/myidsan/apis/federated_auth.go

## Purpose

Implements MyIDSan browser-facing authorization-code login for relying apps.

## Routes

- `GET /api/auth/authorize`: validates client, audience, redirect URI, and MyIDSan session, then redirects back with a one-time code.
- `GET /api/auth/login`: serves the MyIDSan login form (with optional social-login buttons) when authorization starts without a MyIDSan session, via `renderLoginPage` (see below). The `continue` query parameter is carried through the page so a successful social login redirects back to the authorization step.
- `POST /api/auth/login`: authenticates local credentials via `loginPost`, issues the MyIDSan session cookie, and resumes authorization. On failure, re-renders the same branded card (`renderLoginPage` with `errMsg` set and `username` echoed back) instead of returning bare unstyled text — the user only has to retype the password.
- `POST /api/auth/token`: exchanges one authorization code for a signed relying-app token response.

## `renderLoginPage`

Draws the federated sign-in page — the suite's one server-rendered login screen, since
this page sits in the middle of the OAuth redirect chain and must exist before any SPA
loads. It reuses the same visual language as mymatasan's/myseliasan's React
`.login-screen`/`.login-panel` chrome (light-theme color literals, since there is no
stylesheet to import CSS custom properties from here), self-hosted assets only
(`/assets/favicon.svg`, `/assets/fonts.css` — no external font/CDN references, required
because myidsan and the apps that redirect here run on an air-gapped intranet), and
takes `(w, status, continueTo, username, errMsg)`: `status`/`errMsg` let `loginPost`
re-render the card with an inline error on `POST` failure instead of the page always
being a plain `GET` 200; `username` is echoed back into the form on a failed attempt.

## `socialButtonsHTML`

Renders the Google/GitHub buttons, but only for providers that are actually
**configured** — meaning `ClientId` and `ClientSecret` are both non-empty, not merely
that the `login.google`/`login.github` config block is non-nil. The stock
`config.json` ships empty `google`/`github` objects; a non-nil-but-blank provider used
to render a button that sent the browser to `accounts.google.com` with an empty
`client_id` (an OAuth error on the internet, and a dead end on an intranet where
myidsan is deployed — myseliasan's SSO hop lands on this exact page). Covered by
`federated_auth_login_test.go`: `TestSocialButtonsRequireCredentials` (buttons appear
only once both fields are set) and `TestLoginPageHasNoExternalReferences` (the
rendered page references no external host and only same-origin asset paths).

## Security

- Client registration is loaded from `app_auth_config`.
- Callback URLs must match active `app_redirect_uri` rows exactly.
- Authorization codes are random, short-lived, stored in cache, and deleted after token exchange.
- Client secrets are verified against stored SHA-256 hashes.
- Login resume paths reject absolute external URLs.
- The rendered login page loads no external host (no CDN, no Google Fonts) — every asset is a same-origin path, verified by `federated_auth_login_test.go`.
