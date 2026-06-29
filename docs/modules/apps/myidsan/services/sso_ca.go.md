# Module: apps/myidsan/services/sso_ca.go

## Purpose

Implements `ISsoCaService` — myidsan's SSO certificate authority. Manages a singleton CA (certificate + private key) persisted in the `sso_ca` table and issues relying-app client leaf certificates for mTLS. Reuses the `infra/fleetca` CA primitives that back myseliasan's fleet-node mTLS.

## Responsibilities

- `CARootPEM(ctx)` — returns the CA public certificate in PEM form (the trust anchor a relying app or verifier loads). Creates and persists the CA if it does not yet exist.
- `IssueClient(ctx, clientId)` — mints a 1-year ECDSA client leaf with `CN = clientId`, returns `(clientCertPEM, clientKeyPEM, caCertPEM)`. The client private key is not stored.
- `ensure(ctx)` — internal lazy initializer (mutex-protected). Loads the CA from storage on first call; falls back to generating a new CA if the row is missing or the stored PEM is invalid, persisting the result.

## Configuration

| Constant | Value |
|---|---|
| `ssoCaName` | `"default"` — the unique key for the singleton CA row. |
| `ssoCaValidFor` | 10 years |
| `ssoClientCertTTL` | 1 year |

## Notes

- The CA key is stored in the myidsan database (never transmitted off-prem).
- The service is safe for concurrent use; the `ensure` path is protected by a mutex so the CA is generated exactly once.
- Token-endpoint mTLS enforcement is not yet wired in `apphost`; this service is ready for use when that is added.
