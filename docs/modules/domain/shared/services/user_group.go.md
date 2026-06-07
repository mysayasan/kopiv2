# Module: domain/shared/services/user_group.go

## Purpose

Provides user group persistence operations behind the shared service interface.

## Responsibilities

- List user group rows with caller-provided filters and sorters.
- Use newest-first `CreatedAt DESC` ordering when callers do not provide sorters.
- Create, update, and delete user groups through the shared generic repository.
