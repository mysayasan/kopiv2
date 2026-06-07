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

- Group-specific query uses path variable `{id}`.
- POST/PUT enforce strict JSON decode.
