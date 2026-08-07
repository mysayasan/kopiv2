# Module: apps/mypintusan/entities/access_rules.go

## Purpose

The classic access-control triple — group, schedule, grant — plus the holiday calendar, kept
deliberately boring. Every clever access-control data model eventually has to answer "why did
this door open?", and the boring one answers it in a single join.

## Fields

- `AccessGroup` — a named set of holders (`Name` is `ukey`). `SsoGroup`, when set, mirrors
  membership from a `myidsan` group — READ-ONLY and ONE DIRECTION: `myidsan` may drive access,
  access never drives `myidsan`. Nothing yet populates it (see `docs/MYPINTUSAN_DATA_MODEL.md`
  §1 for the general no-email-fallback rule this mirrors).
- `AccessGroupMember` — joins a `Holder` to an `AccessGroup` (`GroupId`/`HolderId`, both
  indexed).
- `Schedule` — a named time policy. `Always` marks the 24/7 case as a flag rather than as seven
  0–1440 windows, so "this grant has no time restriction" reads that way in the UI and in an
  audit export.
- `ScheduleWindow` — one weekly interval within a schedule: `Weekday` (0-6, `time.Weekday`
  numbering, no conversion table needed), `StartMin`/`EndMin` (minutes from midnight). A window
  whose `EndMin` is at or before `StartMin` wraps past midnight — see
  `services/decision.go.md`'s `windowCovers` for the night-shift handling this enables.
- `Holiday` — its own entity on purpose: Malaysian public holidays vary BY STATE, so a site
  needs its own calendar, not a hardcoded national one, and a site with offices in two states
  needs two (`SiteId` scopes the calendar; `0` applies to every site). `Behaviour` is
  `deny`/`follow-sunday`/`ignore` (`Holiday*` constants) — resolved by `services/decision.go.md`'s
  `scheduleAllows`.
- `Grant` — the ACL row: this `GroupId`, through this `DoorId`, during this `ScheduleId`, all
  indexed. Grants are additive — see `services/decision.go.md` gate 8 for why any one matching
  grant is sufficient rather than requiring a conjunction of all of a holder's grants.

## Notes

- Persisted via the shared `dbsql.NewGenericRepo[T]` seam, one repo per entity — both in
  `services/store_sql.go.md`'s `SQLStore` (the decision path's read side: `GrantsFor`,
  `Schedules`, `HolidayOn`) and, as of `apis/access_rules.go.md`, the CRUD surface an operator
  actually edits through (`/api/groups`, `/api/schedules`, `/api/schedules/holidays`,
  `/api/grants`). Test fakes (`services.Store` in `controller_test.go`) still exercise the same
  shape in-memory for the decision-path unit tests.
- `services.Store.GrantsFor(holderId, doorId)` returns the caller-joined grant set for a
  holder's groups at one door; `services.Store.Schedules(ids)` and `HolidayOn(siteId, date)`
  resolve the rest — see `services/controller.go.md`'s `snapshot`.
