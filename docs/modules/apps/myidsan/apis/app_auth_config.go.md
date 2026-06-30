# Module: apps/myidsan/apis/app_auth_config.go

## Purpose

Protected MyIDSan management API for relying-app auth client policy.

## Routes

- `GET /api/app-auth-config`: list auth client configs with secret hashes redacted.
- `POST /api/app-auth-config`: create a client config and hash the supplied `clientSecret`.
- `PUT /api/app-auth-config`: update a client config, preserving the old secret hash unless `clientSecret` is supplied.
- `DELETE /api/app-auth-config/{id}`: delete a client config.

## Security

- Routes use MyIDSan auth and accessrbac middleware (`AccessSessionMidware`). The surface is **RBAC-matrix governed** (not granted by default; a superadmin must explicitly grant the role permission in the matrix to delegate access). This is safe to delegate because it does not expose the raw client secret.
- Read responses expose `hasClientSecret` instead of `clientSecretHash`.
