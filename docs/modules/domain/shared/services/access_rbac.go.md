# Module: domain/shared/services/access_rbac.go

## Purpose

Implements the shared "accessrbac" RBAC core: role CRUD (`accessRoleService`) and a per-endpoint permission matrix (`accessPermissionService`). Both services are app-agnostic and have no `app_code` dimension.

## Types

### AccessPrincipal

The app-agnostic view of an authenticated user that the RBAC middleware needs:

```
RoleId             int64
Disabled           bool
MustChangePassword bool
```

Apps map their own user record to this via `AccessUserResolver`.

### AccessUserResolver (interface)

```go
ResolveAccessUser(ctx context.Context, userId int64) (*AccessPrincipal, error)
```

Returning `(nil, nil)` means "no such user" (treated as signed-out).

## Role Service (IAccessRoleService)

- `EnsureBuiltins(ctx)` — seeds `superadmin` (IsSuperadmin=true, Builtin=true) and `viewer` (Builtin=true) on startup, skipping existing rows.
- `GetByName / GetById` — look up a role; returns `nil, nil` when not found.
- `List` — returns all roles (up to 1000).
- `Create(name, description)` — trims, validates uniqueness, inserts, returns the new row.
- `Update(id, name, description)` — updates mutable fields (name, description, UpdatedAt).
- `Delete(id)` — rejects built-in roles; deletes others.

## Permission Service (IAccessPermissionService)

- `EnsureViewerDefaults(ctx, viewerRoleId)` — seeds a single `/api GET-only` permission row for the viewer role when it has no rows yet.
- `Authorize(ctx, roleId, path, method)` — longest-prefix match over the role's permission rows; no match = `false`. Methods `GET/HEAD/OPTIONS` check `CanGet`; `POST` checks `CanPost`; `PUT/PATCH` check `CanPut`; `DELETE` checks `CanDelete`.
- `ListForRole(ctx, roleId)` — returns all permission rows for a role (up to 1000).
- `Set(ctx, perm)` — upsert by `(roleId, path)`: updates verb flags if the path already exists, inserts otherwise. Normalizes the path (leading slash, no trailing slash, `/` for root).
- `Delete(ctx, id)` — deletes a permission row by ID.

## Constants

- `RoleSuperadmin = "superadmin"` — the name of the built-in superadmin role.
- `RoleViewer = "viewer"` — the name of the built-in read-only role.

## Notes

- Both services are constructed once in `infra/apphost` and passed to apps via `deps.AccessRoles` / `deps.AccessPerms`.
- The enforcement middleware is `domain/utils/middlewares.AccessSessionMidware`.
- The management API surface is `domain/shared/apis/access_rbac.go`.
