# Module: domain/utils/middlewares/rbac.go

## Purpose

Role-based access control middleware for API handlers.

## Authorization Strategy

1. Extract user claims from context.
2. Load role access list from shared cache provider (Redis or in-memory) with read-through fallback to service.
3. Match request by host + path segment boundary.
4. Enforce method permission (`GET/POST/PUT/DELETE`).

## Extra Mutations

For successful `POST` and `PUT`:

- Enforces max payload size (`1MB`).
- Decodes JSON strictly (`DisallowUnknownFields`).
- Adds audit fields:
  - `createdBy`, `createdAt` on POST
  - `updatedBy`, `updatedAt` on PUT

## Deny Conditions

- missing/invalid claims
- no role access mapping
- no matching endpoint access rule
- method permission denied

## Notes

- Access resolution loads role mappings using authenticated `claims.Id` (user id), then caches endpoint lists by `claims.RoleId` for role-scoped reuse.
- Path matching allows an exact endpoint path or a child path such as `/api/admin/test`, but rejects partial prefixes such as `/api/adminx`.
- Host matching supports wildcard `*` and normalizes request hosts that include a port.
