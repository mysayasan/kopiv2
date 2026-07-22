# Module: apps/myidsan/apis/user_login.go

## Purpose

REST API endpoints for user login credential management.

## Route Group

Base path: `/api/user-credential`

- `GET /api/user-credential`
- `GET /api/user-credential/email`
- `PUT /api/user-credential`
- `DELETE /api/user-credential/{id}`

## Middleware Contract

Protected by auth middleware + `AccessSessionMidware` + `RequireSuperadmin`. The entire `/api/user-credential` surface is **superadmin-only** — role assignment is a privilege-escalation vector and must not be reachable by any non-superadmin role regardless of matrix grants.

## Handler Behavior

- GET supports `limit`, `offset`, `filters`, and `sorters` query parameters.
- Filter and sorter query values use the shared SQL enum JSON contract from `query_options.go`.
- Read handlers return myidsan output DTOs through `IUserLoginDtoService`. The output DTO carries no password field, so GET responses (including the account list) never include a stored bcrypt hash.
- PUT decodes the myidsan input DTO, then projects it to a `UserLogin` entity for service writes.
- PUT rejects unknown JSON fields.
- PUT blocks self-role-change: if `body.Id` matches `claims.Id` and `body.UserRoleId` differs from `claims.RoleId`, the request is rejected with 403. This prevents a superadmin from accidentally (or maliciously) demoting or escalating their own role.
- `/email` uses the `email` query parameter for exact unique lookup.
- DELETE parses `{id}` from route params.
