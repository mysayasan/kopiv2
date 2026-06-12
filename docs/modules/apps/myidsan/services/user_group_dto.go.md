# Module: apps/myidsan/services/user_group_dto.go

## Purpose

Adapts the core user group service to return caller-selected DTO types.

## Responsibilities

- Wraps `IUserGroupService` without changing its persistence behavior.
- Projects paginated group lists into the selected DTO type.
- Forwards create, update, and delete calls to the core service.
