# Module: apps/myidsan/apis/identity_status.go

## Purpose

Reports the superadmin handoff state for myidsan so the SPA can show a persistent non-dismissible banner reminding the operator to disable the stock superadmin account once a real one is active.

## Route

`GET /api/identity-status` — requires auth session and the shared accessrbac middleware.

## Response Fields

| Field | Notes |
|---|---|
| `stockSuperadminActive` | True if the configured `localAuth.username` account exists, is active, and holds the superadmin role. |
| `superadminHandoffPending` | True when a stock superadmin is active AND a real (non-stock) superadmin is also active — the safe window to disable the stock account. Drives the SPA banner. |
| `stockEmail` | The email of the stock superadmin (from `localAuth.username`, defaulting to `"superadmin"`). Lets the SPA know which user to highlight in the Users list. |

## Notes

- The stock account is identified by email match against `localAuth.username` (not an `IsStock` flag, since myidsan's `user_login` table does not have that column).
- The banner should be shown any time `superadminHandoffPending` is true, and hidden (or suppressed) when it is false.
