# Module: domain/shared/services/user_role_dto.go

## Purpose

Adapts the core user role service to return caller-selected DTO types.

## Responsibilities

- Wraps `IUserRoleService` without changing its persistence behavior.
- Projects paginated role lists and group-scoped role lookups into the selected DTO type.
- Forwards create, update, and delete calls to the core service.
