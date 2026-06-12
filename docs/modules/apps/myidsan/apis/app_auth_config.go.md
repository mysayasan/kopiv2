# Module: apps/myidsan/apis/app_auth_config.go

## Purpose

Protected MyIDSan management API for relying-app auth client policy.

## Routes

- `GET /api/app-auth-config`: list auth client configs with secret hashes redacted.
- `POST /api/app-auth-config`: create a client config and hash the supplied `clientSecret`.
- `PUT /api/app-auth-config`: update a client config, preserving the old secret hash unless `clientSecret` is supplied.
- `DELETE /api/app-auth-config/{id}`: delete a client config.

## Security

- Routes use MyIDSan auth and RBAC middleware.
- Read responses expose `hasClientSecret` instead of `clientSecretHash`.
