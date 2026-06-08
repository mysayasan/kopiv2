# Module: domain/entities/app_auth_config.go

## Purpose

Stores OAuth-like SSO behavior for a registered relying app.

## Fields

- `appRegistryId`: links the auth config to `app_registry`.
- `clientId`: relying app client identifier used by `/api/auth/authorize` and `/api/auth/token`.
- `clientSecretHash`: SHA-256 hash of the relying app client secret.
- `authCodeTtlSeconds`: per-client authorization-code lifetime override.
- `accessTokenTtlSeconds`: per-client issued-token lifetime override.
- `sessionTtlSeconds`: per-client relying-app session lifetime override.
- `refreshTokenTtlSeconds`, `requirePkce`, and `allowRefreshToken`: reserved policy fields for future stricter client profiles.
- audit fields follow the shared entity convention.

## Notes

- The plaintext client secret is accepted only on write APIs and is not returned by reads.
- A zero TTL inherits global SSO config defaults.
