# Module: domain/shared/services/user_login_dto.go

## Purpose

Adapts the core user login service to return caller-selected DTO types.

## Responsibilities

- Wraps `IUserLoginService` without changing its business behavior.
- Projects list, lookup, and local-authentication results into the selected DTO type.
- Forwards create, update, delete, and local registration calls to the core service.
- Keeps password-safe API DTO projection outside the core entity service.
