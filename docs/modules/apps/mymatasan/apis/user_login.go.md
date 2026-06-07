# Module: apps/mymatasan/apis/user_login.go

## Purpose

Registers app-local user-login read endpoints that return password-safe DTOs.

## Route Group

Base path: `/api/user-login`

- `GET /api/user-login`
- `GET /api/user-login/email`

## Handler Behavior

- Uses auth middleware on the route group and RBAC wrapping per handler.
- `GET /api/user-login` supports `limit`, `offset`, `filters`, and `sorters` using the shared query option parser.
- `GET /api/user-login/email` resolves one user by `email` query parameter.
- Responses are projected through the app output `UserLoginDto`, which omits `userpwd`.
