# Module: domain/shared/apis/user_role.go

## Purpose

REST API endpoints for user role and role-by-group operations.

## Route Group

Base path: `/api/user-credential`

- `GET /api/user-credential`
- `GET /api/user-credential/group/{id}`
- `POST /api/user-credential`
- `PUT /api/user-credential`
- `DELETE /api/user-credential/{id}`

## Middleware Contract

- Auth middleware on route group.
- RBAC wrapper per handler.

## Notes

- List GET supports `limit`, `offset`, `filters`, and `sorters` query parameters.
- Filter and sorter query values use the shared SQL enum JSON contract from `query_options.go`.
- Read handlers return shared output DTOs through `IUserRoleDtoService`.
- POST/PUT decode shared input DTOs, then project them to `UserRole` entities for service writes.
- Group-specific query uses path variable `{id}`.
- POST/PUT enforce strict JSON decode.
