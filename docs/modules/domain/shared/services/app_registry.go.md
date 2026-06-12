# Module: domain/shared/services/app_registry.go

## Purpose

Core shared service for app registry CRUD.

## Behavior

- Uses the generic repository for `entities.AppRegistry`.
- Defaults list sorting to newest first by `CreatedAt`.
- Keeps the service deliberately thin so app registry policy remains in myidsan/apphost wiring.
