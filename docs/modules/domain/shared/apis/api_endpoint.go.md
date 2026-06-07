# Module: domain/shared/apis/api_endpoint.go

## Purpose

REST API endpoints for API endpoint metadata management.

## Route Group

Base path: `/api/endpoint`

- `GET /api/endpoint`
- `POST /api/endpoint`
- `PUT /api/endpoint`
- `DELETE /api/endpoint/{id}`

## Middleware Contract

- Group uses auth middleware.
- Each handler is wrapped by RBAC middleware.

## Handler Behavior

- GET supports paging via `limit` and `offset`.
- GET supports DB-backed filtering and sorting via `filters` and `sorters` JSON query parameters using the shared SQL enum contract from `query_options.go`.
- GET returns shared output DTOs through `IApiEndpointDtoService`.
- POST/PUT decode shared input DTOs, then project them to `ApiEndpoint` entities for service writes.
- POST/PUT parse JSON with unknown fields rejected.
- POST/PUT accept `accessTier` (`0=DevOnly`, `1=AuthOnly`, `2=Public`) as endpoint classification metadata.
- DELETE parses `{id}` from route params.
