# Module: apps/myseliasan/apis/auth.go

## Purpose

Implements MySeliaSan as a relying app for MyIDSan authorization-code login.

## Routes

- `GET /api/auth/start`: creates a local state nonce and redirects to MyIDSan `/api/auth/authorize`.
- `GET /api/auth/callback`: validates state, exchanges code at MyIDSan `/api/auth/token`, and issues the MySeliaSan session cookie.
- `POST /api/auth/logout`: clears the MySeliaSan session cookie.
- `POST /api/auth/local-login` (stock superadmin): authenticates local credentials and issues a session cookie. The issued JWT carries the `Email` claim (falls back to `username` when the account has no real email) so the shared auth middleware's non-empty email check is satisfied. Previously, logging in with the stock superadmin returned HTTP 200 but every subsequent request to `/api/session/me` returned 401 because the email claim was empty.
- `GET /api/auth/config` (public, no auth): returns `{"ssoEnabled": bool}` — `true` when `sso.providerBaseUrl` is non-empty. Lets the login screen hide the "Continue with myidsan" button on a standalone install where federated sign-in cannot work (the packaged/shipped config leaves `sso.providerBaseUrl` empty by default).

## Security

- State is random, cache-backed, and short-lived.
- Callback rejects invalid state before token exchange.
- Token exchange is server-to-server and uses the relying app client secret.
- Token exchange uses the default OS trust store unless `sso.caCertPath` is configured; then MySeliaSan adds that PEM CA/certificate bundle to the HTTPS client trust roots.
- `sso.caCertPath` does not disable TLS verification. Hostname, expiry, and certificate-chain checks still apply.
- `sso.redirectBaseUrl` makes the callback URL stable even when users open the app through another local host alias or proxy host.
- Local session cookies are HttpOnly and issued by shared auth middleware.

## User Provisioning

On successful token exchange, the callback calls `IControlUserService.UpsertFederated(ssoUserId, email, name)` to provision or refresh a `ControlUser` row (kind=`"federated"`). New federated users are assigned the `viewer` role by default. Disabled federated accounts are rejected before the session cookie is issued. The myseliasan session JWT carries the federated user's `ControlUser.Id` and `RoleId`, not the myidsan user ID directly.
