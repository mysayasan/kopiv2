# Module: domain/shared/services/app_registry_dto.go

## Purpose

DTO adapter for shared app registry CRUD.

## Behavior

- Projects `entities.AppRegistry` into caller-selected DTO shape.
- Reuses the core `IAppRegistryService` write methods.
- Supports output DTOs that omit sensitive fields such as `clientSecret`.
