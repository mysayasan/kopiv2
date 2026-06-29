# Module: apps/myidsan/apis/federated_auth.go

## Purpose

Implements MyIDSan browser-facing authorization-code login for relying apps.

## Routes

- `GET /api/auth/authorize`: validates client, audience, redirect URI, and MyIDSan session, then redirects back with a one-time code.
- `GET /api/auth/login`: serves a small MyIDSan login form (with optional social-login buttons) when authorization starts without a MyIDSan session. The `continue` query parameter is carried through the page so a successful social login redirects back to the authorization step. The brand mark is now the shield+keyhole SVG (`socialButtonsHTML` renders Google/GitHub buttons only for providers currently configured and non-nil).
- `POST /api/auth/login`: authenticates local credentials, issues the MyIDSan session cookie, and resumes authorization.
- `POST /api/auth/token`: exchanges one authorization code for a signed relying-app token response.

## Security

- Client registration is loaded from `app_auth_config`.
- Callback URLs must match active `app_redirect_uri` rows exactly.
- Authorization codes are random, short-lived, stored in cache, and deleted after token exchange.
- Client secrets are verified against stored SHA-256 hashes.
- Login resume paths reject absolute external URLs.
