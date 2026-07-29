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
- `refreshTokenTtlSeconds`, `requirePkce`, and `allowRefreshToken`: **RESERVED, NOT ENFORCED.** Nothing reads these three today — `/api/auth/authorize` never parses `code_challenge` and `/api/auth/token` rejects `grant_type=refresh_token` outright. They were removed from the admin API payload/view (`apis/app_auth_config.go.md`) and the Apps form because a persisted-but-ignored security toggle reads as a working one; the columns stay on the entity so enabling PKCE and refresh tokens during the OIDC conformance phase is not a migration (`docs/MYIDSAN_PRODUCTIZATION_PLAN.md` phases 5.3/5.4).
- audit fields follow the shared entity convention.

## Notes

- The plaintext client secret is accepted only on write APIs and is not returned by reads.
- A zero TTL inherits global SSO config defaults.
- Only the entity keeps these three reserved columns; every API surface (admin payload/view, Apps form) omits them, so an operator can never set or observe a value that has no effect.
