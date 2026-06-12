# Module: domain/entities/app_redirect_uri.go

## Purpose

Stores exact callback URLs allowed for one app auth client.

## Fields

- `appAuthConfigId`: links the redirect URI to `app_auth_config`.
- `redirectUri`: exact callback URL accepted by MyIDSan.
- `isActive`: disables a callback without deleting its audit trail.
- audit fields follow the shared entity convention.

## Notes

- Redirect URI matching is exact to avoid open redirect behavior.
