# Module: domain/shared/services/api_endpoint_rbac_dto.go

## Purpose

Adapts the core API endpoint RBAC service to return caller-selected DTO types.

## Responsibilities

- Wraps `IApiEndpointRbacService` without changing RBAC validation behavior.
- Projects paginated RBAC list rows, current-user endpoint joins, and validation results into selected DTO types.
- Supports separate DTO types for base RBAC write/validate rows, enriched admin list rows, and joined current-user endpoint views.
- Forwards create, update, and delete calls to the core service.
