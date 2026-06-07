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
- `/ep/me`: returns endpoint access mapping for current user.
