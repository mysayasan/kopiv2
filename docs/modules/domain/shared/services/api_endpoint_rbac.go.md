# Module: domain/shared/services/api_endpoint_rbac.go

## Purpose

Provides endpoint RBAC data operations and role-access projection for middleware authorization checks.

## Responsibilities

- CRUD operations for endpoint RBAC rows.
- Resolve endpoint access rules by host/path/user role.
- Build joined role-access list for middleware checks.
- Invalidate RBAC role-access cache key namespace on create/update/delete.
- Include endpoint `accessTier` in the joined projection so cached access rows carry endpoint classification metadata.

## Notes

- Cache invalidation failures are logged as warnings and do not block DB writes.
- Middleware reads use read-through cache to keep authorization checks consistent across instances.
