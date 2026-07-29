# Module: domain/utils/middlewares/access_rbac.go

## Purpose

Authorization middleware for the shared "accessrbac" RBAC core. Runs after `AuthMidware` and enforces each app's own role + permission matrix on its own endpoints. Has no `app_code` dimension: each app has its own role tables in its own database.

## Key Type: AccessSessionMidware

`AccessSessionMidware` is constructed by `infra/apphost` during startup (with a `nil` resolver) and passed to the app as `deps.Access`. The app binds its own user store during `RegisterAppRoutes` via `deps.Access.SetResolver(...)`. Because the resolver is set before any request is served, the late-binding is safe.

## Authorization Logic (Middleware method)

1. Extract JWT claims from request context (set by `AuthMidware`).
2. Call `AccessUserResolver.ResolveAccessUser(userId)` to obtain the app's `AccessPrincipal` (role id, disabled flag, must-change-password flag, must-enroll-MFA flag).
3. If the user is disabled or not found, return `403`.
4. If `mustChangePassword` is set, return `403` with the sentinel code `password_change_required`.
5. If `MustEnrollMfa` is set, return `403` with the sentinel code `mfa_enrollment_required` (`MfaEnrollmentRequiredCode`, Productization Phase 3) — checked *after* the password gate so a user owing both is walked through them in a sensible order: set a password you chose, then add a factor to it. `AccessPrincipal.MustEnrollMfa` is the app-agnostic flag; the app decides how it is computed (e.g. `apps/myidsan/app/app.go`'s `userLoginResolver` consults its `EffectiveMfaPolicy` and the account's confirmed-factor state — see `infra/config/config_models.go.md`'s `mfa` block).
6. **Re-stamp `claims.RoleId` from the live principal** so downstream handlers that read the JWT claims see the current role without a re-login. This makes an admin's role change take effect on the user's very next request.
7. Look up the role. If `IsSuperadmin`, pass through unconditionally (bypass the matrix).
8. Otherwise call `IAccessPermissionService.Authorize(roleId, path, method)` — longest-prefix match over the role's permission rows; no match = deny.
9. Return `403` when the matrix denies access.

## Helper Methods

- `CurrentPrincipal(r)` — resolves the request's `AccessPrincipal` (used by the `/api/access-rbac/me` handler).
- `IsSuperadmin(r)` — resolves the caller's role **live from the user store** (not the token's baked `roleId`) so a just-granted or just-revoked superadmin role takes effect without a re-login. Used by management handlers that must self-gate regardless of the matrix.
- `RequireSuperadmin` — gorilla/mux middleware that admits only a superadmin session. Mount it on a subrouter (after `Middleware`) to lock down an entire API surface to superadmin regardless of any matrix grant.
- `SetResolver(users)` — thread-safe rebind of the user resolver (called once during `RegisterAppRoutes`).

## Notes

- Mounted after `auth.Middleware` on protected route groups; not on public routes or the `/api/access-rbac/me` endpoint (which is auth-only, not matrix-gated).
- Fail-closed: if the resolver has not been bound yet, the middleware returns `403` rather than letting the request through.
- `mymatasan` does not use this middleware (it uses local Basic Auth); it opts out via `SharedAPIs()`.
