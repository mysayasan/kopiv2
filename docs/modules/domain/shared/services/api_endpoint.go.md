# Module: domain/shared/services/api_endpoint.go

## Purpose

Provides API endpoint metadata persistence and cache invalidation behavior.

## Responsibilities

- List endpoint rows with caller-provided filters and sorters.
- Use newest-first `CreatedAt DESC` ordering when callers do not provide sorters.
- Create, update, and delete endpoint metadata rows.
- Invalidate RBAC access and endpoint tier cache namespaces after writes.
