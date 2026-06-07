# Module: domain/shared/apis/app_registry.go

## Purpose

Shared CRUD API for registered SSO applications.

## Routes

- `GET /api/app-registry`
- `POST /api/app-registry`
- `PUT /api/app-registry`
- `DELETE /api/app-registry/{id}`

## Behavior

- Protected by shared auth middleware and RBAC middleware.
- Uses shared list filters/sorters for `GET`.
- Accepts `clientSecret` on input but returns the output DTO without the secret.
