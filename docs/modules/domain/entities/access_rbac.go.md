# Module: domain/entities/access_rbac.go

## Purpose

Entity definitions for the shared "accessrbac" RBAC core — the single-app, no-`app_code` authorization layer used by `myidsan` and `myseliasan`.

## Entities

### AccessRole (table: `access_role`)

Represents one authorization role for an app. Fields:

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `Name` | Unique within the app. |
| `Description` | Human-readable summary. |
| `IsSuperadmin` | When true, the middleware bypasses the permission matrix entirely for this role. |
| `Builtin` | Built-in roles (`superadmin`, `viewer`) cannot be deleted. |
| `CreatedBy/UpdatedBy/CreatedAt/UpdatedAt` | Audit fields. |

### AccessRolePermission (table: `access_role_permission`)

One row in the per-role endpoint permission matrix. Path is prefix-matched; the longest matching prefix wins. No matching prefix means deny.

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `RoleId` | Foreign key to `AccessRole.Id`. |
| `Path` | Prefix to match (e.g. `/api/settings`). Normalized to no trailing slash. |
| `CanGet` | Allows `GET`, `HEAD`, `OPTIONS`. |
| `CanPost` | Allows `POST`. |
| `CanPut` | Allows `PUT`, `PATCH`. |
| `CanDelete` | Allows `DELETE`. |
| `CreatedBy/UpdatedBy/CreatedAt/UpdatedAt` | Audit fields. |

## Notes

- Table name uses `access_role` / `access_role_permission` to avoid the SQL reserved word `role`.
- The `app_code` dimension is intentionally absent: each app has its own role catalog in its own database.
- `mymatasan` does not include these entities (it uses local Basic Auth instead of the shared RBAC core).
