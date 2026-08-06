# Module: domain/notification/rollup_migrate.go

## Purpose

`MigrateRollupSourceColumn(ctx, tx *sql.Tx, engine string) error` — the versioned bootstrap
migration that upgrades an existing `notification_rollup` table for the new per-source dimension
(`domain/entities/notification_rollup.go.md`'s `Source` field). Shared here (rather than
duplicated) because **both** apps that fold rollups — `mymatasan` and `myseliasan` — must run it;
each registers it under the identical migration id `20260806-01-notification-rollup-source` in its
own `Migrator.Migrations()` (`apps/mymatasan/app/app.go.md`, `apps/myseliasan/app/app.go.md`).

## Why a migration, not just an additive column

Adding `Source` to the `notification_rollup` unique key (`ukey:"slot"`) changes what counts as
"the same bucket": two rows that used to collapse into one slot (same
camera/category/severity/rule/label, different source) must now be two. The shared bootstrap
engine's additive reconciliation can add a plain column for free, but it does not know to **drop
and rebuild a unique index** — and without that, the very first insert that needs two rows for
what used to be one slot violates the stale index, and the rollup maintainer errors forever from
that point on.

## What it does

1. `rollupTableState` — probes `information_schema.columns` (Postgres/MariaDB) or
   `pragma_table_info` (SQLite) for whether the table exists at all and whether it already has a
   `source` column.
2. **Fresh install** (table doesn't exist yet): no-op — the auto-migrator creates the complete
   shape, `Source` included, straight from the entity.
3. **Existing table, no `source` column**: `ALTER TABLE notification_rollup ADD COLUMN source
   VARCHAR(255)` (`TEXT` on SQLite).
4. **Backfill regardless** of step 3 (an `ADD COLUMN` leaves existing rows `NULL`, and the entity's
   non-pointer `string` field cannot scan a `NULL`): `UPDATE notification_rollup SET source = ''
   WHERE source IS NULL`.
5. `dropRollupSlotIndex` — drops `ux_notification_rollup_slot` so the auto-migrator (which runs
   immediately after every versioned migration, same boot) recreates it **including** `Source`.

## The Postgres constraint-vs-index gotcha (found live)

On Postgres, the unique key may exist either as a bare unique **index** or as a unique
**constraint that owns its own index**, depending on how the table was originally bootstrapped. A
plain `DROP INDEX` on a constraint-owned index fails with `2BP01` ("cannot drop index because
constraint ... requires it"). The fix, verified against a live database: drop the constraint
first (`ALTER TABLE ... DROP CONSTRAINT IF EXISTS ux_notification_rollup_slot`, which takes its
index with it), **then** drop any bare index of the same name (`DROP INDEX IF EXISTS ...`) as a
no-op safety net for the other shape. MariaDB has no `DROP INDEX ... IF EXISTS` on every supported
version, so it probes `information_schema.statistics` first and only issues `ALTER TABLE ... DROP
INDEX` when the index is actually present. SQLite's `DROP INDEX IF EXISTS` handles both cases
natively.

## Notes

- Idempotent end-to-end: re-running it against an already-migrated table finds `hasSource == true`
  and an already-dropped index, and does nothing on either front.
- Runs **before** the auto-migrator (`docs/DB_BOOTSTRAP_SPEC.md`'s "Versioned Migrations"
  ordering), which is what lets it drop the index the auto-migrator is about to rebuild in the same
  boot — the two are two halves of one upgrade, and the ordering is load-bearing.
- See `docs/modules/domain/entities/notification_rollup.go.md` for the entity-level "Upgrading an
  existing database" note this migration exists to satisfy, and
  `docs/modules/domain/notification/baseline.go.md`/`rollup.go.md` (not yet split into their own
  module docs) — the source-aware `Baseline`/`rollupKeyFor` this column backs live in
  `domain/notification/baseline.go` and `domain/notification/rollup.go` respectively.
