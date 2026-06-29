# Module: apps/myseliasan/apis/rbac_admin.go

## Purpose

Superadmin-only HTTP surface for myseliasan-specific user management and the bootstrap superadmin handoff. Role + permission matrix management is delegated to the shared accessrbac module (`/api/access-rbac`).

## Endpoints

All routes require a myseliasan session and the caller's role must be superadmin (checked via `AccessSessionMidware.IsSuperadmin`).

| Method | Path | Body | Notes |
|---|---|---|---|
| `GET` | `/api/rbac/users` | — | List all control-plane users (`ControlUser` rows). |
| `POST` | `/api/rbac/users/{id}/role` | `{roleId}` | Reassign a user's role (must be a valid `access_role.id`). |
| `POST` | `/api/rbac/users/{id}/disabled` | `{disabled}` | Enable or disable a user. |
| `POST` | `/api/rbac/users/{id}/elevate` | — | Bootstrap handoff: promote the target user (must be non-stock and active) to superadmin, then retire all stock accounts. The stock account's next request is rejected by the middleware (disabled). |

## Middleware Contract

- `auth.Middleware` + `session.Middleware` on the `/rbac` subrouter.
- Each handler additionally calls `requireSuper(...)`, a per-handler guard that checks `IsSuperadmin` and returns 403 otherwise.

## Notes

- `elevate` rejects a stock target (`IsStock=true`) to prevent elevating the bootstrap account instead of a real federated user.
- After elevation, the response body includes `retired` (count of stock accounts disabled) and a warning message.
