# Module: apps/myseliasan/apis/session.go

## Purpose

Returns current myseliasan session metadata enriched with the caller's accessrbac authorization state.

## Routes

- `GET /api/session/me`: requires myseliasan auth and returns selected JWT claims plus RBAC state.

## Response Fields

| Field | Notes |
|---|---|
| `userId`, `email`, `name`, `roleId`, `issuer`, `audience` | Standard JWT claims. |
| `roleName` | Resolved from the accessrbac `access_role` table. |
| `isSuperadmin` | True when the role is flagged superadmin; the SPA treats it as a wildcard. |
| `mustChangePassword` | True for the stock bootstrap account until the password is changed. |
| `kind` | `"local"` or `"federated"` (from `ControlUser`). |
| `isStock` | True for the bootstrap account; the SPA uses this to show the handoff prompt. |
| `stockSuperadminActive` | True if an active stock superadmin still exists in the user store. |
| `superadminHandoffPending` | True when a stock superadmin is active AND a real (non-stock) active superadmin also exists — the exact condition when it is safe to disable the stock account. Drives the persistent handoff banner in the SPA. |
| `permissions` | The role's `AccessRolePermission` rows (empty for superadmin; the SPA gates nav tabs on this). |

## Notes

- A user no longer recognized by the `ControlUser` store (e.g. a retired stock account) triggers a 403 so the SPA returns to login.
- The permission array gives the SPA the same data the `AccessSessionMidware` uses for the API gate, so menu visibility matches API access in real time.
