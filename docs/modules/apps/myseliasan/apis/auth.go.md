# Module: apps/myseliasan/apis/auth.go

## Purpose

Implements MySeliaSan as a relying app for MyIDSan authorization-code login.

## Routes

- `GET /api/auth/start`: creates a local state nonce and redirects to MyIDSan `/api/auth/authorize`.
- `GET /api/auth/callback`: validates state, exchanges code at MyIDSan `/api/auth/token`, and issues the MySeliaSan session cookie.
- `POST /api/auth/logout`: clears the MySeliaSan session cookie.

## Security

- State is random, cache-backed, and short-lived.
- Callback rejects invalid state before token exchange.
- Token exchange is server-to-server and uses the relying app client secret.
- Token exchange uses the default OS trust store unless `sso.caCertPath` is configured; then MySeliaSan adds that PEM CA/certificate bundle to the HTTPS client trust roots.
- `sso.caCertPath` does not disable TLS verification. Hostname, expiry, and certificate-chain checks still apply.
- `sso.redirectBaseUrl` makes the callback URL stable even when users open the app through another local host alias or proxy host.
- Local session cookies are HttpOnly and issued by shared auth middleware.
