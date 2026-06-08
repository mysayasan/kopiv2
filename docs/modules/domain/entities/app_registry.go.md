# Module: domain/entities/app_registry.go

## Purpose

Shared entity for apps that participate in kopiv2 SSO.

## Fields

- `code`: stable app code such as `myidsan` or `mymatasan`.
- `audience`: JWT audience value accepted for the app.
- `baseUrl`: operator-facing service URL.
- `clientSecret`: legacy internal relying-app secret field; new browser SSO clients should use `app_auth_config.clientSecretHash`.
- audit fields follow the shared entity convention.

## Notes

- Bootstrapped by myidsan and relying apps.
- Managed through `/api/app-registry`.
- Auth behavior is configured separately through `app_auth_config` and `app_redirect_uri`.
