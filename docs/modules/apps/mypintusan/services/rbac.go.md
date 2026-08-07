# Module: apps/mypintusan/services/rbac.go

## Purpose

Defines mypintusan's authorization catalog and seeds the built-in roles it describes — the same
pattern as `apps/mymatasan/services/rbac.go` and `apps/myiotsan/services/rbac.go`: the shared
mechanics (`domain/shared/services/appliance_rbac.go`'s `EnsureApplianceRoles`) are reused; this
file supplies only the catalog and description genuinely mypintusan's own.

## The role model

Three roles, drawing the line the other appliances draw — **can this person destroy the
record?** — and adding a second, sharper one this app needs that the others do not:
**who may change the rules about who gets in.**

- `viewer` — see doors, readers and their live state, and see the access log. Cannot open
  anything, cannot change who may enter.
- `operator` — + open a door remotely, enrol and revoke credentials, manage holders. **Cannot**
  change grants, schedules, door hardware bindings, or lockdown.
- `admin` (the shared `superadmin` builtin) — everything; bypasses the matrix.

Handing someone a temporary badge is a daily, reversible, fully logged act, and a receptionist
needs it — so credentials and holders are **operator-level**. Editing a `Grant` silently changes
who may enter every door in that group, at every hour, until somebody notices, and nothing about
it looks unusual in a log full of ordinary badge events — so groups/grants/schedules are
**admin-only**, even though the UI would put them two clicks apart from issuing a badge.
**Lockdown is admin-only for the opposite reason**: it is the one control that stops a building
working. It cannot trap anyone — egress is hardware — but an operator who triggers it during a
fire drill has still made an incident out of a nuisance.

## Key Function: Policy

```go
func Policy() []PolicyRule
```

- Everyone signed in: `/api/auth/change-password`.
- **Watching the estate** (viewer + operator, `read`): `/api/doors`, `/api/readers`,
  `/api/events`, `/api/notifications` (the unified feed: alarms, badge decisions, security
  events).
- **Running the building** (operator, `write`; viewer `none`): `/api/doors/unlock` (opening a
  door remotely — a receptionist's daily act, instantaneous, and every use lands in the same log
  as a badge), `/api/holders` (issuing/revoking a badge — the routine, reversible half of access
  control), `/api/notifications/*/read` (marking a feed entry read — viewer gets no write here,
  same shape as the other appliances).
- **Changing the rules** (operator `read` only, viewer `none` — admin-only to write, expressed
  by the absence of a `write` grant below admin): `/api/groups`, `/api/grants`,
  `/api/schedules`.
- **The building's safety posture** (admin-only, listed with no grants so the catalog stays a
  complete description of the surface): `/api/lockdown`, `/api/settings` (users, roles, door
  hardware and system settings — wrong values here do not produce a bad reading, they produce a
  door that opens for the wrong person or an alarm that never comes), `/api/setup`, `/api/pairing`
  (fleet pairing with a `myseliasan` control plane — admin-only because joining or leaving a
  fleet changes who can manage this building's doors remotely).

Every area that grows an API **must** be added here, including the admin-only ones — a route
missing from this catalog is a route nobody can see they are not granting. `admin` is absent by
design: it is a superadmin role and bypasses the matrix entirely.

## Key Function: EnsureRoles

```go
func EnsureRoles(ctx context.Context, roles sharedservices.IAccessRoleService, perms sharedservices.IAccessPermissionService) error {
    return sharedservices.EnsureApplianceRoles(ctx, roles, perms, Policy(), operatorDescription)
}
```

Thin wrapper over the shared mechanics. Called from `app/app.go.md`'s `RegisterAppRoutes`,
**before** the default admin is seeded — the bootstrap admin has to be given the superadmin
role, and the role has to exist to be given.

## Notes

- `RoleAdmin`/`RoleOperator`/`RoleViewer`/`PolicyRule` are aliases onto the shared constants and
  type — identical mechanism to the other three appliances, so role names line up across the
  suite.
- `operatorDescription` — "Day-to-day operator: watch doors, open one remotely, issue and revoke
  badges, manage holders. Cannot change access rules, schedules, door hardware or lockdown."
- `/api/groups`/`/api/grants`/`/api/schedules` are listed in the catalog but **no API currently
  serves them** — there is no `apis/groups.go` or equivalent yet; the rows exist so the matrix
  is complete ahead of the handlers, the same "declare before you build" pattern
  `services/rbac.go.md` in myiotsan used for `/api/devices/*/commands` before P4 shipped it.
