# Module: infra/db/bootstrap/bootstrap.go

## Purpose

Runs the shared database bootstrap process at application startup.

## Responsibilities

- Check whether the target database exists.
- Create the database when allowed.
- Open the target database and verify connectivity.
- Ensure the bootstrap state table exists.
- Validate, order, and run versioned structural migrations (`migration.go`) — before schema
  creation/auto-migrate, so a rename/drop/type-change migration sees the column shape it
  expects (Tier 2 phase M).
- Create missing tables from the entity manifest.
- Add missing columns when safe additive migration is enabled.
- Reconcile unique and non-unique secondary indexes from entity tags.
- Detect schema drift the additive migrator cannot fix — changed column types, columns the
  entities no longer declare (`drift.go`) — and report it, never auto-repair it.
- Persist the manifest hash and manifest JSON.
- Run seeders when enabled.

## Flow

The schema pipeline, in the only order that works (see `migration.go.md` for why migrations
must run first):

1. Normalize bootstrap config.
2. Check/create database.
3. Open target DB.
4. Ensure bootstrap state table.
5. Build manifest from registered entities.
6. Compare stored manifest hash with the current one.
7. **Migrations** (`validateMigrations` → `ensureMigrationTable` → `baselineMigrations` on a
   fresh database, or `runMigrations` otherwise) — structural changes the auto-migrator
   cannot express. A rename has to happen while the column still has its old name; if
   auto-migrate ran first it would `ADD` the entity's new field as an empty column and the
   rename would then fail against a target that already exists.
8. **Auto-migrate** — additive schema updates: create missing tables, `ADD COLUMN`, indexes.
9. **Drift check** (`detectSchemaDrift`, skipped on a fresh database) — reports what neither
   of the above can fix, logged loudly and folded into `Status.Message` when non-empty.
10. Execute seeders if configured.
11. Persist the new manifest state.

"Fresh" (baseline migrations instead of running them, skip the drift check) means the
database has no prior bootstrap-state row for this app — the schema is about to be created
directly from the current entities, so it's already at the latest shape.

## Safety Notes

- No destructive dropping is performed by the additive path.
- Unsafe entity changes are not auto-applied — write a `Migration` (`migration.go.md`)
  instead; `detectSchemaDrift` surfaces what needs one but never fixes it itself.
- The engine is designed for startup bootstrap, not an interactive SQL console.
- Bootstrap currently supports `db.engine=postgres`, `db.engine=mariadb`, and `db.engine=sqlite`.
- SQLite `db_name` is treated as a database file path; `:memory:` is supported for tests/dev experiments.
- SQLite uses file existence for database existence checks and initializes the file with the same bootstrap state manifest flow.
- `ensureIndexes` (formerly `ensureUniqueIndexes`) reconciles both `table.Unique` and `table.Index` through a shared `createIndexes` helper: `CREATE UNIQUE INDEX`/`CREATE INDEX` respectively, `IF NOT EXISTS` on Postgres/SQLite, guarded by a pre-fetched existing-index-name set on MariaDB (which lacks `IF NOT EXISTS` for `CREATE INDEX` on older versions). Default names are `ux_<table>_<cols>` (unique) / `ix_<table>_<cols>` (non-unique) when the entity doesn't supply one.
