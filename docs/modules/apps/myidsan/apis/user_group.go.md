# Module: apps/myidsan/apis/user_group.go

## Purpose

REST API endpoints for user group management.

## Route Group

Base path: `/api/user-group`

- `GET /api/user-group`
- `POST /api/user-group`
- `PUT /api/user-group`
- `DELETE /api/user-group/{id}`

## Handler Behavior

- GET supports `limit`, `offset`, `filters`, and `sorters` query parameters.
- Filter and sorter query values use the shared SQL enum JSON contract from `query_options.go`.
- GET returns myidsan output DTOs through `IUserGroupDtoService`.
- POST/PUT decode myidsan input DTOs, then project them to `UserGroup` entities for service writes.
- POST/PUT reject unknown JSON fields.
- DELETE parses `{id}` from route params.
