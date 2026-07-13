# Module: infra/db/bootstrap/migration.go

## Purpose

Versioned, once-only schema migrations — the escape hatch for what the additive
auto-migrator (`schema.go`/`bootstrap.go`) cannot express. Tier 2 phase M
(`docs/MYMATASAN_TIER2_PLAN.md`).

The auto-migrator can only `ADD COLUMN`. That is enough while a schema only grows, and had
no answer the first time it didn't:

- a **rename** added the new column and silently left all the data stranded in the old one
- a **drop** left the column forever — a legacy `NOT NULL` one eventually breaks inserts
- a **type change** was not even detected — only column *names* were ever compared, so the
  row scanner failed at runtime on a customer's box with no clue why

On a field appliance nobody can reach, there was no supported path for any of those. A
`Migration` is that path. Additive changes still need none — add a field to the entity and
the auto-migrator picks it up.

## Key Type

- `Migration{ID, Name, Statements map[string][]string, Exec func(ctx, *sql.Tx, engine) error}`
  - `ID` — immutable, sortable identity (`"20260714-01-rename-camera-host"`). Migrations run
    in ID order; **never change an ID once released** — it is the key that records the
    migration as applied.
  - `Statements` — SQL keyed by engine. The `""` key applies to every engine; an
    engine-specific key (`"sqlite"` / `"postgres"` / `"mariadb"`) overrides it for that
    engine. The engines genuinely differ here (SQLite could not `DROP COLUMN` before 3.35;
    none of the three agree on how to change a column's type), so a structural migration
    usually has to say what it means per engine rather than pretend they are the same
    database.
  - `Exec` — runs instead of `Statements`, for a change SQL alone cannot express (read rows,
    transform them, write them back). A migration with `Exec` has no stable checksum (a Go
    closure cannot be hashed) — it is recorded with an empty checksum and skipped by the
    tamper check below.

## Responsibilities

- `validateMigrations` — rejects an empty ID, a duplicate ID, or a migration with neither
  `Statements` nor `Exec`. Fails at startup rather than apply non-deterministically.
- `sortMigrations` — orders by ID (lexical == chronological, since IDs are date-prefixed).
- `ensureMigrationTable` — creates the `schema_migration` table
  (`app_name, id, name, checksum, applied_at`, PK `(app_name, id)`) per engine.
- `checksum(engine)` — fingerprints what a migration will actually do (SHA-256 over its
  trimmed statements for that engine). **Tamper detection**: `runMigrations` compares the
  checksum of an already-applied migration against what the code says it should be now, and
  refuses to start ("modified after it was applied — never edit a released migration, add a
  new one") if they differ. Two databases that both claim to have run `"20260714-01"` must
  have had the same thing done to them.
- `baselineMigrations` — records every migration as applied **without running it**. This is
  what a fresh install does: the schema was just created directly from the current entities,
  so it is already at the latest shape — replaying a rename against a column that never had
  the old name would simply fail. Called when the database has no prior bootstrap-state row
  for the app (`freshDatabase` in `bootstrap.go`'s `Ensure`).
- `runMigrations` — applies every pending migration, in ID order, on a database that is
  **not** fresh. Skips ones already recorded (after the checksum check above).
- `applyMigration` — runs one migration inside its own transaction and records it in the same
  transaction, where the engine supports transactional DDL (SQLite, Postgres) — so a failure
  leaves nothing half-done. **MariaDB implicitly commits each DDL statement, so a migration
  there cannot be atomic**: write MariaDB statements defensively (`IF EXISTS`/`IF NOT
  EXISTS`), because a failure part-way through leaves the earlier statements applied and the
  migration unrecorded, so it will be retried on the next boot.
- A migration with no statements for the current engine is legitimate (an engine-specific
  fix) — it is still recorded as applied, so it isn't retried on every boot.

## The pipeline (see `bootstrap.go`)

```
migrations (structural)  ->  auto-migrate (additive)  ->  drift check  ->  seeders (data)
```

Migrations run **before** the additive auto-migrator. That is the only order that works: a
rename has to happen while the column still has its old name. If auto-migrate went first it
would see the entity's new field, `ADD` it as an empty column, and the rename would then fail
against a target that already exists — with the data still stranded in the old column.

## Tests (`migration_test.go`, 9 tests, against a real SQLite database)

- `TestMigration_RenamePreservesData` — the headline: a rename carries the data across (and
  the old column is gone, proving migrations ran before auto-migrate).
- `TestMigration_FreshDatabaseIsBaselinedNotReplayed` — a fresh database records the
  migration as applied without running it (a replay would fail: no old column to rename).
- `TestMigration_AppliedMigrationIsNotRerun` — a second boot does not re-run it.
- `TestMigration_EditingAnAppliedMigrationIsRejected` — the checksum tamper check.
- `TestMigration_RunInIDOrderNotDeclarationOrder` — ID order, not declaration order.
- `TestMigration_InvalidSetsAreRejected` — empty ID, duplicate ID, neither `Statements` nor
  `Exec`.
- `TestMigration_ExecRunsArbitraryGo` — an `Exec` migration doubles a column's values.
- `TestMigration_ResetBaselinesSoTheNextBootDoesNotReplay` — a factory reset (`reset.go`)
  rebuilds a fresh database and must baseline, or the very next boot would replay every
  migration against a brand-new schema and fail — a reset would leave the app unable to
  start.
- `TestMigration_FailureIsAtomicAndNotRecorded` — a mid-migration SQL error leaves neither
  the half-applied schema change nor an "applied" record behind (SQLite, transactional DDL).

## Notes

- `mymatasan` implements `apphost.Migrator` and currently returns `nil`
  (`apps/mymatasan/app/app.go.md`) — no structural changes are pending; the note there is the
  canonical "when do I need one" explanation for app authors.
- See `docs/DB_BOOTSTRAP_SPEC.md` for the "how to write a migration" walkthrough and the full
  pipeline/baselining/checksum/MariaDB-atomicity writeup.
