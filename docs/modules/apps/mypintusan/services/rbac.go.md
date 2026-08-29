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

- Everyone signed in: `/api/auth/session` (read), `/api/auth/change-password`,
  `/api/auth/capabilities` (read — what this role may do, answered by asking this very matrix;
  see `apis/capabilities.go.md`).
- **Watching the estate** (viewer + operator, `read`): `/api/doors`, `/api/readers`,
  `/api/events`, `/api/notifications` (the unified feed: alarms, badge decisions, security
  events), `/api/lockdown` (**seeing** the site is sealed — sealing it is admin-only, and the
  two used to be one rule).
- **Running the building** (operator; viewer `none`): `/api/doors/*/unlock` (opening a door
  remotely — a receptionist's daily act, instantaneous, and every use lands in the same log as a
  badge), `/api/holders` (`readWrite` — issuing and revoking a badge, the routine reversible half
  of access control, plus the list you need to find the person on),
  `/api/notifications/*/read` (marking a feed entry read — viewer gets no write here, same shape
  as the other appliances).
- **Changing the rules** (operator `read` only, viewer `none` — admin-only to write, expressed
  by the absence of a `write` grant below admin): `/api/groups`, `/api/grants`,
  `/api/schedules`.
- **The building's safety posture** (admin-only, listed with no grants so the catalog stays a
  complete description of the surface): `/api/settings` (door hardware and system settings —
  wrong values here do not produce a bad reading, they produce a door that opens for the wrong
  person or an alarm that never comes), `/api/settings/users` and `/api/settings/roles` (their
  own rows, deeper and therefore more specific, so minting an account stays denied even if the
  settings grant is ever widened), `/api/setup`, `/api/pairing` (fleet pairing with a
  `myseliasan` control plane — admin-only because joining or leaving a fleet changes who can
  manage this building's doors remotely), and `/api/deployment` (single-instance on this
  appliance; nothing to grant, but a route absent from the catalog is one nobody can see they are
  not granting).

Every area that grows an API **must** be added here, including the admin-only ones — a route
missing from this catalog is a route nobody can see they are not granting. `admin` is absent by
design: it is a superadmin role and bypasses the matrix entirely.

## What the first test of this catalog found

The catalog had **no test at all** until `services/rbac_test.go` — the only app in the suite whose
policy was never asserted — and it was hiding four defects, three of which made the non-admin half
of the product unusable. They are recorded here because each one is a shape, not an accident:

1. **A rule that matches no route is documentation, not policy.** `/api/doors/unlock` has three
   segments; the route is `/api/doors/{id}/unlock`, which has four. The matcher is segment-wise, so
   the rule governed nothing and the most specific rule that DID match was `/api/doors` — read
   only. **Every remote open by an operator was refused**: the one power the role exists for, the
   one this file argues for at length. `TestPolicy_EveryRuleGovernsARealRoute` now asserts every
   rule against the routes the app really serves, and its mirror asserts the reverse.
2. **`write` grants POST and nothing else.** `/api/holders` was `Operator: write`, so an operator
   could enrol a person and got 403 on the list — the first call the People screen makes. Hence
   `readWrite`, and the comment on it.
3. **The session probe was not in the catalog**, so it was denied by default. A viewer or operator
   signed in (`/api/auth/login` is public and answered 200) and was handed the sign-in card again,
   with no error, permanently. Every endpoint this catalog grants them answered 200 the whole time;
   there was simply no way past the front door of the UI.
4. **Seeing lockdown and setting it were one rule.** The Doors screen loads the door list and the
   lockdown state together, so a refused GET did not hide a pill — it rejected the whole load and
   rendered a permission error where the doors should be. **The home screen of a door controller
   was an error page for every non-admin on every install.**

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
- `/api/groups`/`/api/grants`/`/api/schedules` are now served by `apis/access_rules.go.md`; the
  catalog rows predated the handlers, the same "declare before you build" pattern
  `services/rbac.go.md` in myiotsan used for `/api/devices/*/commands` before P4 shipped it.
- The SCREENS read this matrix rather than re-deriving it. `apis/capabilities.go.md` answers
  `GET /api/auth/capabilities` by calling `Authorize` on the real route each control would use, and
  the SPA renders its navigation rail and every control from that. Before, `views/App.js` filtered
  the rail on a client-side `user.isAdmin` while the server used this catalog — two mechanisms with
  one intent, drifting in both directions at once (a viewer was offered an Unlock button on every
  door card; an operator granted read on grants and schedules had the whole section hidden).
