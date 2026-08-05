# Module: apps/mypintusan/services/store_sql.go

## Purpose

`SQLStore` is the database-backed implementation of `services.Store` (`controller.go.md`), built on
the shared `dbsql.NewGenericRepo[T]` — one repo per entity. It is the production counterpart to the
in-memory test fake (`controller_test.go`'s `memStore`): because `Store` is an interface, the
identical `Controller` and decision path run over either, which is what makes "works offline" a
matter of swapping the store rather than maintaining a second code path that can drift from the
first. `var _ Store = (*SQLStore)(nil)` pins that interchangeability at compile time.

## Responsibilities

- `NewSQLStore(db dbsql.IDbCrud)` — wires one `IGenericRepo[T]` per entity (`readers`, `doors`,
  `creds`, `holders`, `members`, `grants`, `schedules`, `windows`, `holidays`, `events`, `groups`),
  plus `settings`, a repo over the **shared** `sharedentities.RuntimeSetting` table (not part of
  this app's own 12-struct schema — see `schema.go.md` — since the table is registered by the app
  module's shared appliance block, `app/app.go.md`'s `Entities()`).
  `ReaderProfile` has no repo here — nothing in the decision path queries it directly — but it is
  still part of the schema list (`services/schema.go.md`'s `Entities()`).
- `SettingsRepo()` — exposes the `RuntimeSetting` repo to `services.NewAccessSettingsService`
  (`runtime_settings.go.md`), which `app/app.go.md`'s `RegisterAppRoutes` builds it from. Not part
  of the `Store` interface the decision path uses; access settings (timezone, tick, PIN window,
  offline, the bus/reader/SCBK inventory) are an operator-facing concern, not something a badge
  decision reads through this store.
- `maxRows` (5000) — the explicit ceiling passed to every list query in place of an "unbounded"
  option the generic repo does not offer. An access group with more members than this is a
  data-modelling problem to be raised, not silently truncated on the badge path.
- `isNotFound(err)` — the local "row does not exist" test, because `GetById` does **not** return
  `(nil, nil)` for a missing row the way its own doc comment claims (see Notes). Matches the same
  `"no result found"` substring `generic_repo.go`'s unexported `isNoResultErr` checks, since the
  data layer exposes no typed sentinel.
- `eq(field, value)` / `first(rows)` — a single-equality filter helper, and "the one row a
  filtered lookup is expected to produce, or nil". `first` is the reason every child lookup in this
  file goes through `Get` with an explicit filter rather than `GetByForeign`: `GetByForeign` passes
  a **hardcoded `limit=1`** down to `Select` (`infra/db/sql/sqlite/db_crud_sel.go`), so it silently
  returns at most one child row — fine for a genuinely-unique lookup, wrong for a list, and
  indistinguishable between the two at the call site. This store never calls `GetByForeign`.
- `ReaderByBus(ctx, busPort, address)` — resolves the reader enrolled at a bus/address pair; two
  readers sharing one address is treated as an enrolment error (fails closed) rather than binding a
  door to whichever row sorted first.
- `StrikeFor(ctx, door)` — resolves the `services.DoorStrike` (PD address + output channel) that
  opens a door: the address comes from the door's **entry reader** (`door.ReaderInId` →
  `Reader.OsdpAddress`), never from `door.RelayChannel`, which is the output on that same device
  — the two are different numbers, and an earlier version of `BusActuator` conflated them (see
  `controller.go.md`'s Notes for the shipped bug this fixes). Missing `ReaderInId` or a reader
  row that does not exist both fail closed with a descriptive error rather than defaulting to
  address 0. This is the `services.StrikeResolver` `apps/mypintusan/app/runtime.go.md` passes
  into `BusActuator.Resolve` for every controller it builds.
- `Door(ctx, id)` — `GetById`, never `GetByUnique(..., "id", ...)`: a unique-key lookup whose key
  group matches no declared `ukey` falls through to an unfiltered select and returns the first row
  in the table — the bug that once made everyone a superadmin elsewhere in the suite. Missing door
  → `(nil, nil)` via `isNotFound`.
- `CredentialByCard(ctx, format, facility, number)` — matches on all three fields together (format +
  facility code + card number); a duplicate match across holders fails closed with an error rather
  than picking one, since the audit log would otherwise blame a coin toss.
- `Holder(ctx, id)` — `GetById` wrapped the same way; a missing holder (orphaned credential) returns
  `(nil, nil)` so the decision path can deny with `unknown-credential` instead of surfacing a
  database error.
- `GrantsFor(ctx, holderId, doorId)` — two queries, not a join: group memberships for the holder,
  then grants at the door filtered by that group-id set (`sqldataenums.In`). An empty membership set
  returns `nil, nil` rather than falling through to an unfiltered grants query, which would otherwise
  grant every door at the site to an unknown holder.
- `Schedules(ctx, ids)` — deduplicates the requested ids before querying (several grants commonly
  point at the same schedule) and loads both the `Schedule` rows and their `ScheduleWindow` rows by
  `IN`, returning `map[int64]Schedule` / `map[int64][]ScheduleWindow`.
- `HolidayOn(ctx, siteId, date)` — a site-specific holiday row (`SiteId` matching, non-zero) wins
  over a global one (`SiteId == 0`), because Malaysian public holidays vary by state.
- `RecordEvent(ctx, ev)` — a bare `Create`; the log is append-only by design, with no update/delete
  path anywhere in this store.

## Notes

- **Regression coverage for the `GetByForeign` limit-1 trap lives in `store_sql_test.go`**
  (`TestGrantsForReturnsEveryGrant`, `TestSchedulesLoadsAllWindows`), seeding 2+ children and
  asserting all of them come back — not just proving the bug is avoided in code, but that it stays
  avoided.
- **`GetById` does not treat a missing row as `nil`, despite its own doc comment.** On SQLite (and
  the other two drivers), an empty result set from `Select` comes back as an *error*
  (`"no result found"`), and `SelectById`/`GetById` propagate that error straight through rather than
  swallowing it the way `Get` does (via `isNoResultErr`) or the way `SelectByUnique`/`GetByUnique`
  does (an explicit nil-map check that actually fires). `GetById`'s comment — "Not found → nil (not a
  zero-value struct), so `x == nil` checks work" — describes a branch that in practice is dead for
  the not-found case, because the error return happens first. Left unhandled here, a badge against a
  deleted door or an orphaned credential would surface to the controller as a **database failure**
  rather than a denial with a precise reason. `isNotFound` in this file is the workaround;
  `TestMissingRowsAreNotErrors` in `store_sql_test.go` is the regression test. See
  `infra/db/sql/generic_repo.go.md` for the data-layer side of this.
- **Now wired**: `apps/mypintusan/app/app.go.md`'s `RegisterAppRoutes` calls `NewSQLStore(deps.Db)`
  directly, and `apps/mypintusan/app/runtime.go.md` passes it into every `Controller` it builds
  (one per configured OSDP bus). `Entities()` — the 12-struct schema list this app owns — has
  moved out of `store_sql_test.go` into its own file, `services/schema.go.md`, since it is now
  production code the composition root calls rather than a test-only helper.
- `groupsRepo()` exposes the `AccessGroup` repository for administrative writes (creating groups),
  which is not part of the `Store` interface the decision path uses — the decision path only ever
  reads grants and memberships, never groups directly.
