# Module: apps/myseliasan/apis/rbac_admin.go

## Purpose

Superadmin-only HTTP surface for myseliasan-specific user management and the bootstrap superadmin handoff. Role + permission matrix management is delegated to the shared accessrbac module (`/api/access-rbac`).

## Endpoints

All routes require a myseliasan session and the caller's role must be superadmin (checked via `AccessSessionMidware.IsSuperadmin`).

| Method | Path | Body | Notes |
|---|---|---|---|
| `GET` | `/api/rbac/users` | — | List all control-plane users (`ControlUser` rows). |
| `POST` | `/api/rbac/users/{id}/role` | `{roleId}` | Reassign a user's role. `roleId=0` revokes the role (user enters pending/no-access state). Any other value must be a valid `access_role.id`. A superadmin may not change their **own** role (self-targeting is rejected with 403). |
| `POST` | `/api/rbac/users/{id}/disabled` | `{disabled: true/false}` | Enable or disable a user. Disable is blocked for the stock account unless a real (non-stock) active superadmin already exists (`SuperadminStatus` guard) to prevent lockout. |
| `POST` | `/api/rbac/users/{id}/elevate` | — | Bootstrap handoff: promote the target user (must be non-stock and active) to superadmin. The stock account is intentionally left active; a persistent banner in the SPA prompts the operator to disable it from the Users list once the new account is confirmed. A superadmin may not elevate **themselves** (self-targeting is rejected with 403). |

## Middleware Contract

- `auth.Middleware` + `session.Middleware` on the `/rbac` subrouter.
- Each handler additionally calls `requireSuper(...)`, a per-handler guard that checks `IsSuperadmin` and returns 403 otherwise.

## Notes

- `elevate` rejects a stock target (`IsStock=true`) to prevent elevating the bootstrap account instead of a real federated user.
- After elevation, the response body includes `ok: true` and a `warning` message instructing the operator to disable the stock account from the Users list. The `retired` field (and the auto-retire behavior) has been removed; the stock account stays active until explicitly disabled by the operator.
