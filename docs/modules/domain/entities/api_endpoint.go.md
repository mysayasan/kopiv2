# Module: domain/entities/api_endpoint.go

## Purpose

Defines API endpoint metadata used by RBAC and API classification.

## Fields

- `appCode`, `host`, and `path`: app-scoped endpoint identity used by RBAC matching and bootstrap unique keys.
- `accessTier`: API classification from `domain/enums/apiaccess` (`0=DevOnly`, `1=AuthOnly`, `2=Public`).
- audit columns: `createdBy`, `createdAt`, `updatedBy`, `updatedAt`.

## Notes

- `accessTier` classifies routes for rate limiting and other cross-cutting policies; it does not replace auth or RBAC enforcement.
- Dev-only endpoints remain protected when registered through authenticated/RBAC route groups.
- Existing databases created before app-scoped endpoint uniqueness may still contain a host/path-only unique index until manually migrated.
