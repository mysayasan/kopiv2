# Module: domain/shared/apis/user_login.go

## Purpose

REST API endpoints for user login credential management.

## Route Group

Base path: `/api/user-credential`

- `GET /api/user-credential`
- `GET /api/user-credential/email`
- `PUT /api/user-credential`
- `DELETE /api/user-credential/{id}`

## Handler Behavior

- GET supports `limit`, `offset`, `filters`, and `sorters` query parameters.
- Filter and sorter query values use the shared SQL enum JSON contract from `query_options.go`.
- Read handlers return shared output DTOs through `IUserLoginDtoService`.
- PUT decodes the shared input DTO, then projects it to a `UserLogin` entity for service writes.
- `/email` uses the `email` query parameter for exact unique lookup.
- PUT rejects unknown JSON fields.
- DELETE parses `{id}` from route params.
