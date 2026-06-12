# Module: domain/shared/services/dto_projection.go

## Purpose

Provides shared projection helpers used by shared and app-owned DTO adapter services.

## Responsibilities

- Converts one entity or value into the caller-selected DTO type.
- Converts entity slices into DTO slices while preserving service errors.
- Preserves paginated total counts when wrapping list service calls.
- Keeps reflection-based DTO projection in one helper boundary instead of duplicating it in each adapter.
- Exposes the projection wrappers so app modules can reuse the same DTO adapter behavior without importing app-local helpers.
