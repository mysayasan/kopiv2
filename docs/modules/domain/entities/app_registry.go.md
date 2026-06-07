# Module: domain/entities/app_registry.go

## Purpose

Shared entity for apps that participate in kopiv2 SSO.

## Fields

- `code`: stable app code such as `myidsan` or `mymatasan`.
- `audience`: JWT audience value accepted for the app.
- `baseUrl`: operator-facing service URL.
- `clientSecret`: internal relying-app secret input; shared output DTOs do not echo it.
- audit fields follow the shared entity convention.

## Notes

- Bootstrapped by myidsan and mymatasan.
- Managed through `/api/app-registry`.
