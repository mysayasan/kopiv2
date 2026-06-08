# myseliasan

`myseliasan` is a relying control-plane app for `mymatasan`.

It has no public landing page. Opening `/` requires a valid local `myseliasan` session; unauthenticated users are redirected to MyIDSan through the authorization-code flow.

Development defaults:

- App URL: `https://localhost:3002`
- MyIDSan provider URL: `https://localhost:3001`
- Client ID: `myseliasan`
- Dev client secret: `dev-myseliasan-secret`
- Callback URL registered in MyIDSan: `https://localhost:3002/api/auth/callback`
- `sso.redirectBaseUrl` controls the callback origin sent to MyIDSan; it must match a registered MyIDSan redirect URI.
- Local HTTPS requires certificates signed by a CA trusted by the machine running MySeliaSan, because MySeliaSan exchanges callback codes with MyIDSan over HTTPS from the backend.
- `sso.caCertPath` can point to a PEM CA/certificate bundle for that backend call; relative paths resolve from `apps/myseliasan`.
- The default dev value points to `../myidsan/certs/cert.pem`, which trusts the bundled localhost MyIDSan certificate. If you later replace MyIDSan with a privately signed certificate, point `sso.caCertPath` or `SSO_CA_CERT_PATH` at that CA bundle.
- `sso.caCertPath` only adds trusted roots for the backend HTTPS token exchange. It does not skip hostname, expiry, or chain validation.
- DB engine: SQLite at `apps/myseliasan/data/myseliasan.db`

Run MyIDSan first, then run:

```bash
ENVIRONMENT=dev JWT_SECRET=replace-with-strong-secret go run . -app myseliasan
```
