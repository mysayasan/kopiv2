# Module: apps/myiotsan/services/rbac.go

## Purpose

Defines myiotsan's authorization catalog and seeds the built-in roles it describes — the
same pattern as `apps/mymatasan/services/rbac.go`: the shared mechanics
(`domain/shared/services/appliance_rbac.go`'s `EnsureApplianceRoles`/`RolePermissions`) are
reused; this file supplies only the catalog and description that are genuinely myiotsan's.

## The role model

Three roles, drawing the same line mymatasan draws — **can this person destroy the record?**
— because a sensor hub is an evidentiary device too: "the door contact opened at 02:14" is a
fact somebody may want to erase.

- `viewer` — see devices and their current readings, and see that an alert fired. No access to
  the historical record.
- `operator` — + review telemetry history, acknowledge alerts. Cannot actuate a device, delete
  readings, or change rules and settings.
- `admin` (the shared `superadmin` builtin) — everything; bypasses the matrix, no catalog rows.

**A second line myiotsan has that mymatasan does not: ACTUATION.** A camera is read-mostly,
but an IoT device gets written to, and a bad relay write is physically dangerous in a way a
bad PTZ move is not — so actuation is admin-only here, on top of the per-device capability
toggle the plan (`docs/MYIOTSAN_PLAN.md` §3.4) calls for. This rule was written into the catalog
in P0, *before* the command path that would exercise it existed (shipped P4,
`services/commands.go.md`), and it must NOT be loosened without a deliberate decision that an
operator may open doors.

**Seeing what was done is not the same power as doing it.** Reading a device's command history
and its desired-vs-reported twin is granted to viewer/operator even though issuing a command is
admin-only — an audit trail visible only to the people who could have written to it is not an
audit trail. This is expressible because the matrix is most-specific-path-wins: the narrower
`/api/devices/*/commands/history` and `/api/devices/*/twin` rows grant `read` while the shorter
`/api/devices/*/commands` prefix stays admin-only (no grants).

## Key Function: Policy

```go
func Policy() []PolicyRule
```

Returns the catalog, now including the P1/P2 device/telemetry/rules/notification surface added
alongside the ingest spine:

- Everyone signed in: `/api/auth/change-password`.
- **Watching the estate** (viewer and operator both `read`): `/api/devices` (devices + their
  CURRENT readings), `/api/profiles`, `/api/rules`, `/api/alerts`, `/api/notifications`, and
  (home-automation) `/api/scenes` and `/api/schedules` — a viewer can see that a "Goodnight"
  scene or a sunset schedule exists and what it does, the same way they can see an alert rule,
  without being able to fire it. This is the live picture, and it is all a viewer gets.
  `/api/kb` (the in-app knowledge base, `apps/myiotsan/kb`) is granted here too — it is read-only
  reference content compiled into the binary, so there is no reason to gate it any tighter than the
  device/profile catalog it documents.
- **Reviewing the record** (operator and up only): `/api/devices/*/readings` (Viewer: `none`,
  Operator: `read`) — THIS is the viewer/operator line, and it is mymatasan's line exactly: a
  viewer sees what is happening now, only an operator can go back through the telemetry
  history. A sensor hub is an evidentiary device too — "the door contact opened at 02:14" is a
  fact somebody may want to erase. `/api/devices/*/commands/history` and `/api/devices/*/twin`
  (both Viewer: `read`, Operator: `read`, added P4) sit in this tier too — seeing what was done
  to a device is not the same power as doing it.
- **Operating** (operator only, `write`): `/api/alerts/*/ack` (acknowledge an alert),
  `/api/notifications/*/read` (mark a notification read).
- Admin only (listed with no grants, so the catalog stays a complete description of the API
  surface even where nothing below admin is granted): `/api/devices/*/password` (rotate a
  device's broker credential), `/api/discovery` (P3 — open an enrollment window and adopt new
  devices; the ONE act in the whole app that lets an unknown thing talk to the broker at all, so
  an operator does not get to do it either), `/api/settings/users`, `/api/settings/roles` (now
  actually served — see `apis/settings.go.md`), `/api/devices/*/commands` (P4 — issuing a command;
  written into the catalog here in P0, before the command path itself existed), and
  (home-automation) `/api/scenes/*/run` (running a scene commands every device in it through the
  exact same actuation path as a single command, so it inherits the same admin-only rule),
  `/api/schedules/*/run` (test-firing a schedule) and `/api/settings/location` (the site
  latitude/longitude a sunrise/sunset schedule needs to compute its fire time — configuration
  that decides WHEN an automation fires, gated the same as the automation itself). Also admin
  only, added alongside the tabbed Settings page: `/api/settings/notification` (webhook/telegram
  delivery credentials — where alerts LEAVE the box), `/api/settings/telemetry` (storage
  retention and the embedded broker's listen address — wrong here and every device stops
  connecting), and `/api/system` (a restart). **Also added, closing a catalog gap that predated
  the Settings page**: `/api/pairing` (fleet key, claim code, adoption status) had never been
  listed at all — the matrix already denied it to viewer/operator by default, but a route the
  catalog does not name is a route nobody can see they are not granting, which is exactly what
  this catalog exists to prevent. **Also admin only (Flow Engine)**: `/api/flows` (the WHOLE area
  — a flow can run arbitrary sandboxed JavaScript and, through an output node, actuate a device via
  the same guarded command path everything else uses, so unlike scenes/schedules even READING a
  flow's graph is admin-only) and `/api/flows/*/run` (test-firing a flow, which may actuate for
  real). Also NOT granted
  to anyone below admin, and therefore correctly absent from any `read`/`write` row: creating and
  deleting devices, editing profiles (which could widen a deadband until a sensor effectively
  stops recording), writing rules, and (home-automation) authoring a scene or a schedule
  (`POST`/`PUT`/`DELETE /api/scenes`, `/api/schedules`) — only reading them is granted below
  admin; the matrix's default-deny leaves authoring admin-only without a listed row. A bad relay
  write is physically dangerous in a way a bad PTZ move is not.

Every phase that adds an API area MUST add it here, INCLUDING the admin-only areas — a route
missing from this catalog is a route nobody can see they are not granting.

## Key Function: EnsureRoles

```go
func EnsureRoles(ctx context.Context, roles sharedservices.IAccessRoleService, perms sharedservices.IAccessPermissionService) error {
    return sharedservices.EnsureApplianceRoles(ctx, roles, perms, Policy(), operatorDescription)
}
```

Thin wrapper over the shared mechanics — see `domain/shared/services/appliance_rbac.go.md`.
Called from `app.go`'s `RegisterAppRoutes`, before the default admin is seeded (the bootstrap
admin has to be given the superadmin role, and the role has to exist to be given).

## Notes

- `RoleAdmin`/`RoleOperator`/`RoleViewer`/`PolicyRule` are aliases onto the shared constants
  and type from `domain/shared/services/appliance_rbac.go` — identical mechanism to
  mymatasan's own aliases, so both apps' role names line up.
- `operatorDescription` — "Day-to-day operator: monitor devices, review telemetry,
  acknowledge alerts. Cannot actuate devices, delete readings, or change settings."
