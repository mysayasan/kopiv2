# Module: apps/myseliasan/entities/control_user.go

## Purpose

Entity for a myseliasan control-plane user. Stored in table `control_user`.

## Fields

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `Kind` | `"local"` (stock admin, username + bcrypt password) or `"federated"` (myidsan-authenticated, keyed by `SsoUserId`). |
| `Username` | Unique for local accounts; empty for federated. |
| `SsoUserId` | myidsan user ID; unique for federated accounts; zero for local. |
| `Email` | Email from myidsan (federated) or empty (local). |
| `Name` | Display name. |
| `PasswordHash` | bcrypt hash; JSON-omitted (`json:"-"`). |
| `RoleId` | Foreign key into `access_role` (the shared accessrbac role table). |
| `MustChangePassword` | Stock superadmin must set a real password on first login. |
| `Disabled` | Disabled users are rejected by `AccessSessionMidware`. |
| `IsStock` | True for the bootstrap account created by `EnsureStockSuperadmin`. |
| `LastLoginAt` | Unix timestamp of last successful login. |
| `CreatedAt / UpdatedAt` | Audit timestamps. |

## Notes

- Uniqueness for username (local) and SsoUserId (federated) is enforced by the service, not a DB unique key, since each field is empty for the opposite kind.
- `RoleId` points to the shared `AccessRole` table; role lookup and matrix enforcement use `deps.AccessRoles` / `deps.Access`.
- After the bootstrap handoff (`POST /api/rbac/users/{id}/elevate`), the stock account is disabled; any session it held is immediately rejected by the middleware.
