# Module: infra/login/config.go

## Purpose

Builds OAuth provider configurations and shared OAuth state helpers.

## Responsibilities

- Build Google OAuth2 config from app config.
- Build GitHub OAuth2 config from app config.
- Generate per-request OAuth state values.
- Store OAuth state in HTTP-only callback cookies.
- Validate callback state before token exchange.

## Notes

- OAuth config is returned per provider instead of stored in global mutable state.
- State cookies are scoped to the provider callback path and expire after five minutes.
