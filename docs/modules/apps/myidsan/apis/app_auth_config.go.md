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

## Notes

- `appAuthConfigPayload`/`appAuthConfigView` no longer carry `refreshTokenTtlSeconds`, `requirePkce`, or `allowRefreshToken`. The columns still exist on `entities.AppAuthConfig` (see `domain/entities/app_auth_config.go.md`), but nothing in `/api/auth/authorize` or `/api/auth/token` has ever read them — no `code_challenge` parsing, and `grant_type=refresh_token` is rejected outright — so accepting and echoing them back told an operator a security control was configured when it was inert. `appAuthConfigPayloadToEntity` writes those three fields as the Go zero value rather than passing through an operator-supplied value. They come back, enforced, with OIDC conformance work (`docs/MYIDSAN_PRODUCTIZATION_PLAN.md` phases 5.3/5.4).
