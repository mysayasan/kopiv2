# Module: domain/utils/middlewares/access_rbac.go

## Purpose

Authorization middleware for the shared "accessrbac" RBAC core. Runs after `AuthMidware` and enforces each app's own role + permission matrix on its own endpoints. Has no `app_code` dimension: each app has its own role tables in its own database.

## Key Type: AccessSessionMidware

`AccessSessionMidware` is constructed by `infra/apphost` during startup (with a `nil` resolver) and passed to the app as `deps.Access`. The app binds its own user store during `RegisterAppRoutes` via `deps.Access.SetResolver(...)`. Because the resolver is set before any request is served, the late-binding is safe.

## Authorization Logic (Middleware method)

1. Extract JWT claims from request context (set by `AuthMidware`).
2. Call `AccessUserResolver.ResolveAccessUser(userId)` to obtain the app's `AccessPrincipal` (role id, disabled flag, must-change-password flag).
3. If the user is disabled or not found, return `403`.
4. If `mustChangePassword` is set, return `403` with the sentinel code `password_change_required`.
5. Look up the role. If `IsSuperadmin`, pass through unconditionally (bypass the matrix).
6. Otherwise call `IAccessPermissionService.Authorize(roleId, path, method)` — longest-prefix match over the role's permission rows; no match = deny.
7. Return `403` when the matrix denies access.

## Helper Methods

- `CurrentPrincipal(r)` — resolves the request's `AccessPrincipal` (used by the `/api/access-rbac/me` handler).
- `IsSuperadmin(r)` — quick boolean check used by management handlers that must self-gate regardless of the matrix.
- `SetResolver(users)` — thread-safe rebind of the user resolver (called once during `RegisterAppRoutes`).

## Notes

- Mounted after `auth.Middleware` on protected route groups; not on public routes or the `/api/access-rbac/me` endpoint (which is auth-only, not matrix-gated).
- Fail-closed: if the resolver has not been bound yet, the middleware returns `403` rather than letting the request through.
- `mymatasan` does not use this middleware (it uses local Basic Auth); it opts out via `SharedAPIs()`.
