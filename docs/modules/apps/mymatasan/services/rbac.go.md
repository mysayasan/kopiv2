# Module: apps/mymatasan/services/rbac.go

## Purpose

Defines mymatasan's authorization catalog and seeds the built-in roles it describes. This is
the ONLY place mymatasan's authorization surface is described — deliberately Go data rather
than rows an installer clicks into a grid, because a policy you can review in a diff is a
policy somebody actually reviews.

## The role model

Three roles. The line between them is **"can this person destroy evidence?"**

- `viewer` (`RoleViewer`, the shared `viewer` builtin) — watch live and see that an alert
  fired. No access to recorded footage.
- `operator` (`RoleOperator`, mymatasan's own role) — + review and download footage,
  acknowledge alerts, PTZ, talk-back. Cannot delete or purge anything, cannot change AI rules
  or settings, cannot add or remove cameras.
- `admin` (`RoleAdmin`, the shared `superadmin` builtin) — everything. A superadmin role
  bypasses the matrix entirely (see `apis.NewRequireRolePermission`), so it has no rows in
  the catalog.

Three and no more, by design: every extra role is a support burden and a matrix the customer
will misconfigure. The matrix stays editable via `perms.Set` for a site that genuinely needs
a fourth.

## Key Types

```go
type verbs struct{ Get, Post, Put, Delete bool }

type PolicyRule struct {
    Path        string
    Description string
    Viewer      verbs
    Operator    verbs
}
```

`Path` is matched **segment-wise** by the shared matcher, and `"*"` matches exactly one
segment — this is what lets an action be granted without granting its whole collection:
`"/api/cameras/*/ptz"` lets an operator move a camera without letting them create one. The
**most specific matching rule decides** (rules do not union), so a broad grant can be carved
out by a narrower one — e.g. `"/api/recording"` is readable while nothing beneath it is
deletable, because delete is never granted anywhere in the catalog.

## Key Function: Policy

```go
func Policy() []PolicyRule
```

Returns the full catalog, grouped by what it governs: change-own-password (everyone),
watching live, seeing that something happened (alerts/notifications/capacity/settings/setup
reads), reviewing recorded footage (operator and up — the evidentiary line), operating
(acknowledge/PTZ/talk, operator only), and admin-only areas (onvif, training, teach, anomaly,
pairing, system, user/role management) listed with **no grants** so the catalog stays a
complete description of the API surface — an area missing from it is an area nobody can see
they aren't granting, and the admin UI renders this list.

`admin` is absent from the catalog by design (see above).

## Key Function: rolePermissions

```go
func rolePermissions(roleId int64, roleName string) []sharedentities.AccessRolePermission
```

Renders the catalog into permission rows for one role. **Writes a row for every rule,
including all-false ones — this is load-bearing, not clutter.** An all-false row under a
granted prefix is how a carve-out is expressed (`/api/settings` readable,
`/api/settings/users` not, because it is deeper and therefore more specific). An earlier
version skipped empty rows; a test caught that a viewer could then enumerate every user
account, because the broader `/api/settings` grant governed with nothing narrower to shadow
it.

## Key Function: EnsureRoles

```go
func EnsureRoles(ctx context.Context, roles sharedservices.IAccessRoleService, perms sharedservices.IAccessPermissionService) error
```

1. `roles.EnsureBuiltins(ctx)` seeds `superadmin` + `viewer`.
2. Creates the `operator` role if it does not already exist.
3. For `viewer` and `operator`, seeds `rolePermissions(...)` from the catalog — but **only
   when the role currently has zero permission rows**. A role with any existing rows is
   assumed to be a site's deliberate tuning and is never clobbered on reboot. The consequence
   is that a role stripped all the way to zero permissions gets its defaults back on the next
   boot, which is the correct outcome: a role with no permissions cannot sign anyone in to
   anything.

Called from `app.go`'s `RegisterAppRoutes`, before the default admin is seeded and before
`ILocalUserService.BackfillRoles` — both need the roles to exist first (see
`apps/mymatasan/app/app.go.md`).

## Notes

- `RoleAdmin = sharedservices.RoleSuperadmin`, `RoleViewer = sharedservices.RoleViewer` —
  aliases onto the shared builtins so mymatasan's role names line up with the rest of the
  suite. `RoleOperator = "operator"` is mymatasan-specific.
- Tested end-to-end (not just the matcher in isolation) by
  `apps/mymatasan/services/rbac_test.go`: seeds the real catalog through the real permission
  service and matcher, then asserts every boundary — nobody below admin can destroy evidence
  or reconfigure, viewer can watch live and nothing else, operator can do the job, an unknown
  route is denied, a roleless user is denied, and the catalog is well-formed.
