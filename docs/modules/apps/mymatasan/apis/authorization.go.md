# Module: apps/mymatasan/apis/authorization.go

## Purpose

`NewRequireRolePermission` is mymatasan's write-and-read authorization middleware: every
protected request is decided against the signed-in user's role permission matrix.

It replaces `NewRequireAdminForWrites`, which was one bool:

```go
if user.IsAdmin || isReadOnlyMethod(r.Method) || isViewerWriteAllowed(r.URL.Path)
```

Three things were wrong with that:

- **No read authorization at all.** Every GET was allowed to any signed-in user, so a
  non-admin could read every camera, every recording, and every alert snapshot in the
  system.
- **"May watch cameras but may not delete recordings" was not expressible** — the property
  that makes an NVR evidentiary rather than a camera viewer.
- **The viewer allow-list matched by `strings.HasSuffix`**, so any future route ending in
  `/read` or `/ack` would become silently writable by a non-admin — a booby trap for
  whoever added the next endpoint.

## Key Function: NewRequireRolePermission

```go
func NewRequireRolePermission(
    roles sharedservices.IAccessRoleService,
    perms sharedservices.IAccessPermissionService,
) func(http.Handler) http.Handler
```

Must be registered AFTER `apis.NewLocalBasicAuth`, so the authenticated `AuthenticatedUser`
is already in request context.

Decision order:

1. No principal in context → `401`-equivalent (`ErrLimitedAccess`, "not authenticated"). Fail
   closed — the auth middleware should always have populated this.
2. `user.IsAdmin` → allowed. This is the one place "admin gets everything" still lives, and
   it is now an explicit flag on a **role** (the role's `IsSuperadmin`), not an independent
   bool on the user — see `AuthenticatedUser.IsAdmin`, which `services.identity()` derives
   from the resolved role.
3. `user.RoleId <= 0` → denied ("your account has no role assigned"). A user with no role has
   no permissions; this is the only safe reading of an account an administrator has not
   finished setting up.
4. `perms.Authorize(ctx, user.RoleId, r.URL.Path, r.Method)` decides everyone else against
   the role's permission matrix (deny-by-default, most-specific-row-wins — see
   `domain/shared/services/access_rbac.go.md`). A matrix lookup **error fails closed** and is
   logged (`log.Printf`) — an authorization check that cannot run is not a reason to let the
   request through.

## Notes

- Deliberately does not reimplement path matching or specificity here — all of that lives in
  the shared `accessPermissionService.Authorize` (`domain/shared/services/access_rbac.go`).
  This middleware is just the glue between mymatasan's Basic-auth principal and the shared
  matrix.
- The catalog the built-in roles (`viewer`, `operator`) are seeded from is
  `apps/mymatasan/services/rbac.go`'s `Policy()`. `admin` bypasses the matrix entirely (step 2
  above), so it needs no rows.
- mymatasan reuses the shared RBAC **tables and services** (`AccessRole`,
  `AccessRolePermission`, `IAccessRoleService`, `IAccessPermissionService`) but not the shared
  `AccessSessionMidware` middleware, which hard-requires JWT claims mymatasan does not have
  (it uses Basic auth + a session cookie, not a JWT). Injecting synthetic JWT claims into a
  security middleware just to reuse it was rejected as the kind of shortcut that becomes a
  CVE — this middleware talks to the shared permission service directly instead.
- `apis/control_dispatch.go`'s tunneled parent→node commands go through the same matrix: the
  synthetic principal it builds carries a `RoleId` resolved from the asserted role name, and
  this middleware treats it exactly like a locally-authenticated request.
