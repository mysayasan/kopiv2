# Module: apps/myidsan/entities/sso_ca.go

## Purpose

Entity for myidsan's persisted SSO certificate authority. Stored in table `sso_ca`. A singleton row (`Name = "default"`) holds the CA certificate and private key used to issue relying-app mTLS client certificates.

## Fields

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `Name` | Unique key (`ukey1`); always `"default"` for the singleton CA. |
| `CertPem` | PEM-encoded CA certificate (public trust anchor). |
| `KeyPem` | PEM-encoded CA private key (never transmitted outside myidsan). |
| `CreatedBy / UpdatedBy` | Audit user IDs (0 = system). |
| `CreatedAt / UpdatedAt` | Audit timestamps (Unix). |

## Notes

- Mirrors the design of myseliasan's fleet CA (`ControlSetting.pairing.caCert`/`caKey`), using the same `infra/fleetca` primitives.
- The CA is created lazily on first call to `ISsoCaService.CARootPEM` or `ISsoCaService.IssueClient`; it is regenerated automatically if the stored PEM is corrupt or unloadable.
- Issued client leaf certificates carry CN = the relying app's `client_id`; the private key is returned to the caller once and is never stored.
