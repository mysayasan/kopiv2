# Module: apps/myidsan/apis/sso_cert.go

## Purpose

Exposes myidsan's SSO certificate authority for relying-app mTLS client authentication. Allows administrators to retrieve the CA public certificate and to issue client certificates (CN = the app's `client_id`) that a relying app can present for future token-endpoint mTLS.

## Routes

All routes require a myidsan auth session and the shared accessrbac middleware.

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/sso-ca` | Returns `{ caCertPem }` — the CA public certificate in PEM form. Relying apps load this as their trust anchor. |
| `POST` | `/api/sso-ca/issue/{id}` | Issues a client leaf certificate for the `AppAuthConfig` row with the given `{id}`. The `AppAuthConfig.ClientId` becomes the certificate CN. Returns `{ clientId, caCertPem, clientCertPem, clientKeyPem }`. The private key is returned only once and is never stored. |

## Notes

- The CA is a singleton created lazily on first use (`ensure` in `ISsoCaService`). A corrupt or missing stored CA is regenerated automatically.
- Token-endpoint mTLS enforcement (requiring the client cert on the `/api/auth/token` route) is **not yet implemented**; `apphost`'s TLS listener has no client-cert hook. These endpoints are available for operators who want to prepare relying apps in advance.
- Built on `infra/fleetca`, the same CA primitives used by the myseliasan fleet-node mTLS.
