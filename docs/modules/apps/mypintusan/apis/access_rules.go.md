# Module: apps/mypintusan/apis/access_rules.go

## Purpose

The groups/schedules/grants surface — the classic access-control triple, exposed at last. Until
this file existed a person created through the wizard could not badge through ANY door: the
decision path (correctly, fail-closed) requires a grant, grants require a group and a schedule,
and nothing served any of them. This file is what closes that gap.

The file's own header explains the admin-only stance: `services/rbac.go.md` treats credential
issuance as operator-level because it is daily, reversible and logged, but a grant edit silently
changes who may enter every door in a group, indefinitely, and "looks like nothing in a log" —
which is why every write here is gated by `requireAdmin` on top of the shared matrix, AND every
grant/membership mutation publishes a notification naming the administrator who made it.

## Responsibilities

- `NewAccessRulesApi(router, db, notify)` — builds one `dbsql.IGenericRepo[T]` per entity
  (`AccessGroup`, `AccessGroupMember`, `Schedule`, `ScheduleWindow`, `Holiday`, `Grant`, plus
  read-only lookups against `Door` and `Holder` for name hydration) and registers three
  subrouters: `/groups`, `/schedules` (with `/schedules/holidays` registered before the numeric
  `/{id}` route — order that reads as intent even though the `[0-9]+` constraint would never
  collide with the literal `holidays` segment), and `/grants`.
- `requireAdmin(w, r)` — the explicit gate mirroring `doors.go`'s `create`: returns the acting
  user's id/display name and `false` (having already written the error response) if the caller is
  not `IsAdmin`. Every write handler in this file starts with it.
- `ruleChanged(ctx, actor, body)` — publishes an `access.rule-change` `Info` notification
  (`domain/notification`), body `"<what happened> — by <actor>"`. This is the direct answer to the
  admin-only header comment: the change that "looks like nothing in a log" now generates a named
  audit row in the same feed an operator already watches, and — on an adopted node — travels up
  the fleet control channel to `myseliasan` (`app/wire_fleet.go.md`).

### Groups

- `listGroups` / `createGroup` (name required, trimmed) / `deleteGroup`.
- `deleteGroup` **refuses** while any `Grant` references the group ("this group still grants
  access to doors; delete its grants first") — deleting it anyway would leave grant rows pointing
  at a ghost, matching nothing, which reads as a bug rather than an intentional revoke. Group
  memberships, by contrast, are cascade-deleted with the group: they mean nothing without it.
- `listMembers` hydrates each `AccessGroupMember` with `HolderName` via a per-row `holders.GetById`
  lookup (group membership lists are small, so this stays free of join plumbing) — the screen
  never has to show "holder 4471".
- `addMember` validates the group and the holder both exist, then treats a duplicate
  `(GroupId, HolderId)` row as a no-op returning the existing membership (grants are additive, so
  a duplicate would only double-count, not change behaviour).
- `removeMember` looks up the membership by `(Id, GroupId)`, resolves both names for the audit
  body, then deletes it.

### Schedules and holidays

- `listSchedules` returns each `Schedule` with its `Windows` embedded (`scheduleWithWindows`).
- `createSchedule` validates: a schedule that is neither `always` nor has at least one
  `ScheduleWindow` matches NOTHING — every grant through it would deny out-of-schedule forever,
  which reads as a broken door rather than a broken rule, so it is rejected outright. Each window's
  `Weekday` must be 0–6 and `StartMin`/`EndMin` must be within 0–1440; **`EndMin <= StartMin` is
  explicitly VALID** and means the window wraps past midnight (the night shift) — only the raw
  bounds are checked, not the ordering.
- `deleteSchedule` refuses while any `Grant` references it ("grants still use this schedule;
  delete or repoint them first"), then cascade-deletes its `ScheduleWindow` rows.
- `listHolidays` / `createHoliday` / `deleteHoliday` — a holiday's `Date` must parse as
  `YYYY-MM-DD`; `Behaviour` must be `entities.HolidayDeny` / `HolidayFollowSunday` / `HolidayIgnore`,
  defaulting to `HolidayDeny` when omitted (`entities/access_rules.go.md`, resolved by
  `services/decision.go.md`'s `scheduleAllows`).

### Grants

- `listGrants` returns each `Grant` as a `grantRow` hydrated with `GroupName`/`DoorName`/
  `ScheduleName` — the names an operator actually reads; the raw ids stay for the UI.
- `createGrant` resolves all three legs (`group`, `door`, `schedule`) and 404s if any is missing
  **before** writing the row — a grant pointing at a mistyped id would look configured and do
  nothing, exactly the half-configured state this app keeps refusing to create elsewhere
  (`doors.go.md`'s `create` makes the same argument for door+reader). A duplicate
  `(GroupId, DoorId, ScheduleId)` grant is returned as the existing row rather than inserted again
  — grants are additive/OR, so two identical rows change nothing except an auditor's confidence.
- `deleteGrant` resolves the group/door names for the audit body, then deletes the row.

## Notes

- Live-bench verified: an existing grant produced a `GRANTED` badge decision; `DELETE`ing that
  grant via the API made the next badge at the same door/holder `DENIED` with reason `no-grant`;
  re-`POST`ing the grant made the next badge `GRANTED` again. Both the delete and the create
  produced a named `access.rule-change` notification in the feed.
- The first-run wizard (`views/react-webpack/src/views/Wizard.js`) is the primary caller of this
  surface on a fresh install: after the first person is created it builds (or reuses, by name) an
  "Everyone" group and an "Always" 24/7 schedule, adds the new holder to the group, and grants the
  group the door the wizard just created — driven live via CDP end to end, including a second run
  that reused rather than duplicated the pre-existing group/schedule.
- The frontend surface is `views/react-webpack/src/views/Access.js` (admin-only nav item, "Access
  rules"), backed by `views/react-webpack/src/lib/access.js`'s typed API functions. Server
  refusals (e.g. "grants still reference it") are shown to the operator verbatim rather than
  translated into a generic error.
