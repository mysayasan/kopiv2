# Module: domain/shared/services/runtime_log_dto.go

## Purpose

Adapts the core runtime log service to return caller-selected DTO types.

## Responsibilities

- Wraps `IRuntimeLogService` without changing log retention behavior.
- Projects runtime log list results into the selected DTO type.
- Forwards monthly and retention delete calls to the core service.
