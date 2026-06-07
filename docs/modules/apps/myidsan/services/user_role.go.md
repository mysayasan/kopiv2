# Module: apps/myidsan/services/user_role.go

## Purpose

Provides user role persistence operations behind the shared service interface.

## Responsibilities

- List user role rows with caller-provided filters and sorters.
- Use newest-first `CreatedAt DESC` ordering when callers do not provide sorters.
- Resolve roles by group foreign key.
- Create, update, and delete user roles through the shared generic repository.
