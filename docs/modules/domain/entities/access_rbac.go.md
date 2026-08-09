# Module: domain/entities/access_rbac.go

## Purpose

Entity definitions for the shared "accessrbac" RBAC core — the single-app, no-`app_code` authorization layer. `myidsan` and `myseliasan` use the full core (JWT-session middleware + these tables); `mymatasan` (and `myiotsan`) migrate and authorize against the same `AccessRole`/`AccessRolePermission` tables through their own local-auth middleware instead — see the Notes below.

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
| `Managed` | True when this row was DERIVED from the role's page grants (`services.DerivePermissions`) rather than typed in by hand under "Advanced" in the RBAC UI. Re-deriving a role replaces only its managed rows, so a hand-written exception is never silently destroyed by a page edit. New column; not yet written by any caller in this commit — `DerivePermissions` and `IAccessRolePageService` set it, but nothing invokes them outside tests yet. |
| `CreatedBy/UpdatedBy/CreatedAt/UpdatedAt` | Audit fields. |

### AccessRolePage (table: `access_role_page`)

One row is one role holding one page at one access level — what an administrator actually chose,
as opposed to `AccessRolePermission`, which is what the server enforces. `AccessRolePermission`
rows for a page-managed role are derived from these; this table is the record of intent, the
permission table is the record of enforcement. New in this commit; not yet populated by any
caller outside tests — see `domain/shared/services/page_access.go.md` and `role_page.go.md`.

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `RoleId` | Foreign key to `AccessRole.Id`. |
| `PageId` | Stable page id from an app's page catalog (e.g. mymatasan's `PageLiveViews`). |
| `Level` | Page level id (e.g. `"view"`, `"use"`, `"manage"`). Levels are cumulative — holding a level implies every level declared before it in the catalog. |
| `CreatedBy/UpdatedBy/CreatedAt/UpdatedAt` | Audit fields. |

## Notes

- Table name uses `access_role` / `access_role_permission` / `access_role_page` to avoid the SQL reserved word `role`.
- The `app_code` dimension is intentionally absent: each app has its own role catalog in its own database.
- `mymatasan` migrates and uses `AccessRole`/`AccessRolePermission` in its own local database — it shares the role/permission **data model** (one authorization model for the suite, which `myiotsan` also inherits) and authorizes through it via its own middleware (`apis.NewRequireRolePermission`), but not the shared JWT-session middleware, since mymatasan authenticates with local HTTP Basic Auth instead of a JWT (see `apps/mymatasan/services/rbac.go.md`). It does not currently migrate `AccessRolePage` — its page catalog (`apps/mymatasan/services/pages.go`) is proven equivalent to its existing `Policy()` catalog by tests, but nothing derives from pages at boot yet, so the table is not yet part of any app's migration list.
