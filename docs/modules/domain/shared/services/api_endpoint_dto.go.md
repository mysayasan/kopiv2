# Module: domain/shared/services/api_endpoint_dto.go

## Purpose

Adapts the core API endpoint service to return caller-selected DTO types.

## Responsibilities

- Wraps `IApiEndpointService` without changing endpoint catalog behavior.
- Projects paginated endpoint lists into the selected DTO type.
- Forwards create, update, and delete calls to the core service.
