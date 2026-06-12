# Module: domain/shared/apis/api_endpoint_rbac.go

## Purpose

REST API endpoints for RBAC rule management and self-validation queries.

## Route Group

Base path: `/api/endpoint-rbac`

- `GET /api/endpoint-rbac`
- `GET /api/endpoint-rbac/validate/me`
- `GET /api/endpoint-rbac/ep/me`
- `POST /api/endpoint-rbac`
- `PUT /api/endpoint-rbac`
- `DELETE /api/endpoint-rbac/{id}`

## Middleware Contract

- Auth middleware on group.
- RBAC wrapper on all handlers.

## Special Endpoints

- `/validate/me`: checks access rule validity for host/path against current user role.
- `/ep/me`: returns endpoint access mapping for current user, including joined endpoint `metadata` for dynamic client navigation.

## List Query Behavior

- `GET /api/endpoint-rbac` supports `limit`, `offset`, `filters`, and `sorters` query parameters.
- `GET /api/endpoint-rbac` returns enriched list DTOs with joined endpoint host/path/app metadata and role title so administration screens do not need to display raw foreign keys.
- Filter and sorter query values use the shared SQL enum JSON contract from `query_options.go`.
- Read handlers return shared output DTOs through `IApiEndpointRbacDtoService`, including enriched admin list DTOs and joined current-user endpoint DTOs.
- POST/PUT decode shared input DTOs, then project them to `ApiEndpointRbac` entities for service writes.
