# Module: domain/shared/services/api_log_dto.go

## Purpose

Adapts the core API log service to return caller-selected DTO types.

## Responsibilities

- Wraps `IApiLogService` without changing log retention behavior.
- Projects paginated API log rows into the selected DTO type.
- Forwards create and retention delete calls to the core service.
