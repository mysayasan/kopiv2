# Module: apps/myseliasan/services/rbac.go

## Purpose

Implements `IControlUserService`, the myseliasan-specific identity layer over the shared accessrbac role core. Handles both local (stock bootstrap) and federated (myidsan-authenticated) control-plane users.

## Responsibilities

- `EnsureStockSuperadmin(ctx, username, password)` — seeds a local bootstrap admin (must-change-password, IsStock=true) on startup. Refreshes the password from config only while the account is still untouched (MustChangePassword=true, not Disabled). Once a real admin has changed the password or the account is retired, config no longer overrides it.
- `UpsertFederated(ctx, ssoUserId, email, name)` — provisions or refreshes a myidsan user on first login. New accounts get the `viewer` role. Returns `ErrUserDisabled` for a disabled account.
- `AuthenticateLocal(ctx, username, password)` — bcrypt verification; returns `ErrInvalidCredentials` or `ErrUserDisabled`.
- `ChangePassword(ctx, userId, current, next)` — local accounts only; minimum 8 characters; clears `MustChangePassword`.
- `SetRole(ctx, userId, roleId)` / `SetDisabled(ctx, userId, disabled)` — superadmin-level mutations.
- `RetireStock(ctx)` — disables every IsStock account. Returns count retired (retained in the interface for programmatic use; the elevate endpoint no longer calls it automatically).
- `SuperadminStatus(ctx)` — returns `(stockActive bool, realActive bool, err error)`. `stockActive` is true if any enabled stock superadmin exists; `realActive` is true if any enabled non-stock superadmin exists. Used by the disable-lockout guard in the API handler and by the `/api/session/me` response to drive the handoff banner.
- `ResolveAccessUser(ctx, userId)` — implements `sharedservices.AccessUserResolver`; maps `ControlUser → AccessPrincipal`.
- `List(ctx)` — returns all users (up to 1000).

## Notes

- Built-in roles (`superadmin`, `viewer`) are seeded by the shared accessrbac core (`EnsureBuiltins`), not by this service.
- The `ErrUserDisabled` and `ErrInvalidCredentials` sentinel errors are checked by the auth API to return appropriate HTTP responses.
- Minimum password length is 8 characters (`minPasswordLen`).
