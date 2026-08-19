# Module: apps/mymatasan/services/rbac.go

## Purpose

Defines mymatasan's authorization catalog and seeds the built-in roles it describes. This is
the ONLY place mymatasan's authorization surface is described — deliberately Go data rather
than rows an installer clicks into a grid, because a policy you can review in a diff is a
policy somebody actually reviews.

The role/catalog **mechanics** — creating the built-in roles, rendering a catalog into
permission rows, seeding a fresh role in full and backfilling a catalog addition into an
already-configured one without touching what a site tuned — moved to
`domain/shared/services/appliance_rbac.go` (`EnsureApplianceRoles`/`RolePermissions`) so
myiotsan runs the same machinery instead of a second copy. What stays here, and is genuinely
mymatasan's, is the **catalog itself** — `Policy()`, which says what a camera NVR's viewer and
operator may actually reach.

## The role model

Three roles. The line between them is **"can this person destroy evidence?"**

- `viewer` (`RoleViewer`, the shared `viewer` builtin) — watch live and see that an alert
  fired. No access to recorded footage.
- `operator` (`RoleOperator`, alias of `sharedservices.RoleOperator`) — + review and download
  footage, acknowledge alerts, PTZ, talk-back. Cannot delete or purge anything, cannot change
  AI rules or settings, cannot add or remove cameras.
- `admin` (`RoleAdmin`, alias of `sharedservices.RoleAdmin` = the shared `superadmin`
  builtin) — everything. A superadmin role bypasses the matrix entirely (see
  `apis.NewRequireRolePermission`), so it has no rows in the catalog.

Three and no more, by design: every extra role is a support burden and a matrix the customer
will misconfigure. The matrix stays editable via `perms.Set` for a site that genuinely needs
a fourth.

## Key Types

`PolicyRule` is a re-export alias of `sharedservices.PolicyRule` (`domain/shared/services/appliance_rbac.go`),
so the catalog below reads as mymatasan's own:

```go
type PolicyRule = sharedservices.PolicyRule // { Path, Description string; Viewer, Operator Verbs }
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

Returns the full catalog, grouped by what it governs: read-your-own-session and
change-own-password (everyone), watching live, seeing that something happened
(alerts/notifications/capacity/settings/setup reads), reviewing recorded footage (operator and
up — the evidentiary line), operating (acknowledge/PTZ/talk, operator only), and admin-only
areas (onvif, **faces**, training, teach, anomaly, pairing, system, user/role management,
**the audit trail**) listed with **no grants** so the catalog stays a complete description of
the API surface — an area missing from it is an area nobody can see they aren't granting, and
the admin UI renders this list.

`{Path: "/api/evidence", ..., Viewer: none, Operator: write}` — exporting footage as a
verifiable evidence bundle is an OPERATOR capability, same as reading `/api/recording`, while
deleting stays denied at every level. That is the same evidentiary line drawn twice: an
operator who was present at an incident must be able to hand the footage of it to somebody,
and must not be able to destroy it. Every export is audited with the operator's stated reason
(`apps/mymatasan/services/evidence_export.go.md`).

`{Path: "/api/audit", ..., Viewer: none, Operator: none}` — admin-only and read-only for
everyone (there is no delete route at all in `apis/audit.go`). The trail names who watched,
downloaded and deleted which footage, so it is itself sensitive: a record of people's viewing,
not just of configuration.

`{Path: "/api/faces", ...}` (no grants) was added after the page-catalog test in
`apps/mymatasan/services/pages_test.go` (`TestPages_GrantOnlyPathsThePolicyGoverns`) found face
recognition governed by no rule at all: absent from this catalog, it was denied by default
(correct, since deny-by-default already refuses an ungoverned path) but invisible in the admin
UI that renders this list — an area nobody could see they were not granting, the exact failure
this catalog's completeness rule exists to prevent. See `apps/mymatasan/services/pages.go.md`
for the `people` page (`AdminOnly`) that now names it.

`{Path: "/api/auth/session", ..., Viewer: read, Operator: read}` is the first rule in the
catalog and is load-bearing in a way most rules are not: it is what the SPA calls first, before
it renders anything, to learn who is signed in and whether they must change their password. Its
absence from the catalog was a real bug — deny-by-default refused it, so `viewer` and
`operator` accepted the password and then hit "you do not have permission for this action"
before ever reaching the UI; only `admin`/`superadmin` worked, because superadmin bypasses the
matrix and never exercises this path. Fixed by adding the row here, and (for every install that
had already seeded its roles before the fix) by `EnsureApplianceRoles` backfilling missing
catalog rows into existing roles instead of skipping a role that has any rows at all — see
`domain/shared/services/appliance_rbac.go.md`.

`admin` is absent from the catalog by design (see above).

## Key Function: EnsureRoles

```go
func EnsureRoles(ctx context.Context, roles sharedservices.IAccessRoleService, perms sharedservices.IAccessPermissionService) error {
    return sharedservices.EnsureApplianceRoles(ctx, roles, perms, Policy(), operatorDescription)
}
```

A thin wrapper: all the seeding mechanics (builtins, the `operator` role, seed-in-full-or-backfill)
now live in `sharedservices.EnsureApplianceRoles` — see
`domain/shared/services/appliance_rbac.go.md`. This function's only job is to supply
mymatasan's own `Policy()` and `operatorDescription`.

Called from `app.go`'s `RegisterAppRoutes`, before the default admin is seeded and before
`ILocalUserService.BackfillRoles` — both need the roles to exist first (see
`apps/mymatasan/app/app.go.md`).

## Notes

- `RoleAdmin = sharedservices.RoleAdmin`, `RoleOperator = sharedservices.RoleOperator`,
  `RoleViewer = sharedservices.RoleViewer` — aliases onto the shared constants so mymatasan's
  role names line up with the rest of the suite (and with myiotsan's own aliases of the same
  constants).
- `rolePermissions`, the function that used to render the catalog into permission rows, is
  gone from this file — that's now `sharedservices.RolePermissions`, called internally by
  `EnsureApplianceRoles`.
- Tested end-to-end (not just the matcher in isolation) by
  `apps/mymatasan/services/rbac_test.go`: seeds the real catalog through the real permission
  service and matcher, then asserts every boundary — nobody below admin can destroy evidence
  or reconfigure, viewer can watch live and nothing else, operator can do the job, an unknown
  route is denied, a roleless user is denied, and the catalog is well-formed.
  `TestPolicy_NonAdminRolesCanCompleteSignIn` is the regression test for the `/api/auth/session`
  bug above: it walks the exact sequence App.js calls before it can render
  (`GET /api/auth/session`, `GET /api/settings/runtime`, `GET /api/cameras`,
  `POST /api/auth/change-password`) and asserts viewer and operator are allowed every step —
  every other test in the file asserts what a role must NOT do, this one asserts the floor.
