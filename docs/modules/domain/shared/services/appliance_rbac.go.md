# Module: domain/shared/services/appliance_rbac.go

## Purpose

The appliance three-role model's **mechanics**, shared by every appliance app (`mymatasan`,
`myiotsan`). New in the local-auth extraction: `apps/mymatasan/services/rbac.go` used to own
both the catalog (what each role may reach) and the machinery that turns a catalog into
seeded permission rows; this file is the machinery, factored out so a second appliance app
does not reimplement it.

## The role model

Three roles, and the line between them is **"can this person destroy evidence?"**

- `viewer` — watch/see the current state and that an alert fired. No access to the historical
  record.
- `operator` (`RoleOperator = "operator"`) — + review the record, acknowledge alerts, operate
  the device. Cannot delete or purge anything, cannot change rules or settings. **Not** a
  shared builtin: `EnsureApplianceRoles` creates it per app, `superadmin`/`viewer` are seeded
  by the underlying `IAccessRoleService.EnsureBuiltins`.
- `admin` (`RoleAdmin = RoleSuperadmin`) — everything. A superadmin role bypasses the matrix
  entirely, so it needs no catalog rows.

What each role may actually *reach* is per-app (mymatasan governs cameras and recordings,
myiotsan governs sensors and telemetry) — that is `PolicyRule`, supplied by each app's own
`services.Policy()`. What is shared is turning that catalog into seeded permission rows.

## Key Types

```go
type Verbs struct{ Get, Post, Put, Delete bool }

type PolicyRule struct {
    Path        string
    Description string
    Viewer      Verbs
    Operator    Verbs
}
```

`Path` is matched segment-wise by the shared matcher (`access_rbac.go`), and `"*"` matches
exactly one segment. The **most specific matching rule decides** (rules do not union).
`VerbsRead`/`VerbsWrite`/`VerbsNone` are convenience literals for the common cases.

## Key Function: RolePermissions

```go
func RolePermissions(roleId int64, roleName string, policy []PolicyRule) []entities.AccessRolePermission
```

Renders one app's catalog into permission rows for one role. **Writes a row for every rule,
including all-false ones — this is load-bearing, not clutter.** An all-false row under a
granted prefix is how a carve-out is expressed. Skipping empty rows lets a broader grant
govern with nothing narrower to shadow it — a real bug once, caught by a live test (a viewer
could enumerate every user account).

## Key Function: EnsureApplianceRoles

```go
func EnsureApplianceRoles(
    ctx context.Context,
    roles IAccessRoleService,
    perms IAccessPermissionService,
    policy []PolicyRule,
    operatorDescription string,
) error
```

1. `roles.EnsureBuiltins(ctx)` seeds `superadmin` + `viewer`.
2. Creates the `operator` role if it does not already exist, described by
   `operatorDescription` (what a human sees next to it).
3. For `viewer` and `operator`, seeds from `RolePermissions(...)`. A role with **zero**
   permission rows is seeded in full. A role that already has rows is **backfilled**: a row is
   written only for a catalog path the role has no row for at all — an existing row's verbs are
   never touched, so a site's deliberate tuning survives every reboot. Adding a row can only
   ever match the catalog's own stated intent (including a deliberately all-false row that
   carves a narrower denial out of a broader grant), and can never revoke a verb a site granted,
   because a row that already exists is never edited.

   This replaced an earlier all-or-nothing rule — skip the whole role once it has ANY rows —
   that protected a site's tuning but had a real cost: a rule *added* to the catalog after a
   site's roles were first seeded would never reach that install, only brand-new ones. That
   is exactly how the appliance once shipped a corrected catalog rule (`/api/auth/session` on
   `apps/mymatasan/services/rbac.go`) that no already-running install would ever receive without
   this backfill.

Called from each app's own `EnsureRoles` wrapper (`apps/mymatasan/services/rbac.go`,
`apps/myiotsan/services/rbac.go`), which supplies that app's `Policy()` and
`operatorDescription`. Must run before the default admin is seeded and before
`ILocalUserService.BackfillRoles` — both need the roles to exist first. Runs on every boot, so
the backfill applies retroactively the first time an upgraded binary starts.

## Notes

- `RoleAdmin`/`RoleOperator` here are the canonical names; each app re-exports them as its own
  `RoleAdmin`/`RoleOperator`/`RoleViewer` aliases so its catalog reads as "its own" while
  staying byte-identical to the shared constants.
- Tested end-to-end by each app's own `rbac_test.go` (seeds the real catalog through the real
  permission service and matcher, asserts every boundary), not just here in isolation — the
  catalogs differ per app and each app's tests own asserting its own boundaries.
- `EnsureApplianceRoles` itself is unit-tested here, against stub `IAccessRoleService`/
  `IAccessPermissionService` implementations, in `appliance_rbac_test.go`: an empty role is
  seeded in full; a row a site tuned is never written to again (asserted both by value and by
  recording every `Set` call); a path added to the catalog after a role was first seeded
  reaches that role on the next boot (the regression test); and a second boot against an
  unchanged catalog writes nothing at all.
