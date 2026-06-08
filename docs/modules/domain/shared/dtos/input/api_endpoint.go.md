# Module: domain/shared/dtos/input/api_endpoint.go

## Purpose

Defines the shared input DTO for API endpoint create/update payloads.

## Notes

- Mirrors `entities.ApiEndpoint`; `appCode` is required for app-scoped RBAC endpoint catalogs.
- Accepts `metadata` as JSON text for endpoint presentation settings such as menu `id`, `label`, `group`, `order`, `summary`, and `tone`.
