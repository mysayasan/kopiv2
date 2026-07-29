# Module: domain/shared/apis/access_rbac.go

## Purpose

Shared, superadmin-only HTTP management surface for the accessrbac RBAC core: role CRUD and per-role endpoint permission matrix management. Mounted by `infra/apphost` when `SharedAPIConfig.AccessRbac` is true (the default). The same routes are identical on every app that uses the shared module (myidsan, myseliasan).

## Route Group

Base path: `/api/access-rbac`

### Auth-only (no matrix gate)

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/access-rbac/me` | Returns the caller's `userId`, `email`, `roleId`, `roleName`, `isSuperadmin`, `mustChangePassword`, `mustEnrollMfa`, `pending`, and `permissions` array. `userId` and `email` are sourced from the JWT claims so the SPA can identify "self" (e.g. avoid offering to disable your own account). Superadmin returns `isSuperadmin: true` with an empty permissions array (the SPA treats it as a wildcard). `pending: true` when the user is authenticated but has no role assigned (awaiting admin clearance); the SPA shows an "access pending" screen. `mustEnrollMfa` (Productization Phase 3) mirrors `mustChangePassword`: `true` pins the SPA to a second-factor enrollment screen when the app's MFA policy requires one and this account has none; always `false` for apps with no MFA policy. Used by the SPA to compute menu visibility from the same rules that gate the APIs. |

### Superadmin-only (session auth + matrix + extra `IsSuperadmin` self-gate)

| Method | Path | Body | Notes |
|---|---|---|---|
| `GET` | `/api/access-rbac/roles` | — | List all roles. |
| `POST` | `/api/access-rbac/roles` | `{name, description}` | Create a role. |
| `PUT` | `/api/access-rbac/roles/{id}` | `{name, description}` | Update a role's name/description. |
| `DELETE` | `/api/access-rbac/roles/{id}` | — | Delete a non-builtin role. |
| `GET` | `/api/access-rbac/permissions?roleId=` | — | List permission rows for a role. |
| `POST` | `/api/access-rbac/permissions` | `{roleId, path, canGet, canPost, canPut, canDelete}` | Upsert a permission row (insert or update by path). |
| `DELETE` | `/api/access-rbac/permissions/{id}` | — | Delete a permission row. |

## Middleware Contract

- `/api/access-rbac/me` is protected by `auth.Middleware` only (auth-only; every authenticated user may read their own authorization state).
- The management routes (roles + permissions) use `auth.Middleware` + `access.Middleware` on the subrouter, then a per-handler `super()` guard that checks `IsSuperadmin` and returns 403 otherwise.

## Notes

- User→role assignment is NOT handled here; it stays app-specific (each app has its own user store and binds its own `AccessUserResolver`).
- `mymatasan` opts out of this endpoint group by returning `SharedAPIConfig{Version: true}` from `SharedAPIs()`; the routes are not mounted for that app.
