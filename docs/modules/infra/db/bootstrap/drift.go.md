# Module: infra/db/bootstrap/drift.go

## Purpose

Detects schema differences the additive auto-migrator (`schema.go`/`bootstrap.go`) cannot
fix, and reports them — it never repairs anything. Tier 2 phase M
(`docs/MYMATASAN_TIER2_PLAN.md`), the companion to `migration.go`.

Before this, only column *names* were ever compared against the entities. So a **type
change** (`int64` -> `string` on an entity field) was not detected at all — the column kept
its old SQL type and the row scanner failed at runtime, on a customer's box, with no clue
why — and a **dropped** field left its column in the database forever, invisible. Rewriting a
column's type in place is destructive and engine-specific, which is exactly what a
`Migration` is for; this module's job is to make the drift loud enough that a developer
writes that migration instead of discovering it from a support ticket.

## Key Types

- `DriftKind` — `DriftTypeChanged` (the column exists but its type no longer matches the
  entity) or `DriftExtraColumn` (the database has a column the entity no longer declares — a
  field renamed or removed without a migration; its data is still there).
- `SchemaDrift{Kind, Table, Column, Expected, Actual}` — one detected difference.
  `String()` renders a human-readable line naming the fix ("write a migration to change
  it" / "write a migration to drop or rename it"), used in the `apphost` boot log.

## Responsibilities

- `detectDrift(ctx, db, engine, table)` — compares one table's declared columns (from the
  reflected entity manifest) against what the database actually has
  (`describeColumns`/`describeSQLiteColumns`, via `information_schema.columns` on
  Postgres/MariaDB or `PRAGMA table_info` on SQLite). Never modifies anything.
  - A column present on the entity but missing from the database is **not** drift — the
    additive auto-migrator adds it.
  - A column present on the database but not on the entity is `DriftExtraColumn`.
  - A column present on both, with a differing type *family* (see `typeFamily` below), is
    `DriftTypeChanged`.
  - Auto-increment primary keys are skipped: `columnDefinition` rewrites them wholesale
    (`BIGSERIAL` / `INTEGER PRIMARY KEY AUTOINCREMENT` / `BIGINT AUTO_INCREMENT`), so their
    declared type says nothing useful about what's on disk.
- `typeFamily(sqlType)` — normalizes an SQL type string to the broad class that actually
  decides whether a row scan works: `bool`, `int`, `text`, `float`, `time`, `blob`, `json`
  (or the trimmed/lowercased type itself as a fallback). Comparing exact type strings across
  three engines is hopeless (`TEXT` vs `text` vs `VARCHAR(255)`, `BIGINT` vs `int8`) —
  comparing families catches the drift that actually breaks a row scan without crying wolf
  over cosmetic differences.
- `detectSchemaDrift(ctx, db, engine, manifest)` (in `bootstrap.go`) — the entry point:
  iterates every table in the manifest, skips ones that don't exist yet (a fresh table is a
  job for auto-create, not drift), and collects `detectDrift` results across all of them.

## The critical subtlety: compare against what THIS engine actually stores

The comparison is `typeFamily(normalizeSQLType(column.SQLType, engine))` against
`typeFamily(got.DataType)` — the type this specific engine **actually gets**, not the
manifest's engine-neutral type. SQLite stores a `BOOLEAN` column as `INTEGER` and MariaDB
stores it as `TINYINT(1)`; comparing the raw manifest type would report false drift on every
bool column, on every boot, on two of the three engines. A warning that always fires is a
warning nobody reads. `TestDrift_EngineTypeMappingsDoNotCryWolf` is the regression test for
exactly this.

## When it runs (see `bootstrap.go`)

Only on a database that is **not** fresh (`freshDatabase` is false), after auto-migrate has
had its chance to add any missing columns, and before seeders run. Results land in
`Status.SchemaDrift`, fold into `Status.Message`
(`"%d schema difference(s) the auto-migrator cannot fix — a migration is needed"`), and are
logged loudly both inside `bootstrap.go` and again by `infra/apphost/run.go` after `Ensure`
returns.

## Tests (`drift_test.go`, 6 tests, against a real SQLite database)

- `TestDrift_TypeChangeIsDetected` — an `int`-family column that became `text` is reported.
- `TestDrift_ExtraColumnIsDetected` — a column the entity no longer declares is reported.
- `TestDrift_MatchingSchemaReportsNone` — a correct schema reports zero drift.
- `TestDrift_MissingColumnIsNotDriftItIsAdded` — a genuinely missing column is not drift; the
  additive auto-migrator's job, not this module's.
- `TestDrift_EngineTypeMappingsDoNotCryWolf` — the BOOLEAN-stored-as-INTEGER/TINYINT(1)
  regression test described above.
- `TestTypeFamily_NormalizesAcrossEngines` — `typeFamily` unit coverage across the
  int/text/float/time/blob/json/bool classes and their engine-specific spellings.

## Notes

- Drift is **reported, never auto-repaired** — see `migration.go.md` for the mechanism that
  fixes it.
- Distinct from the pre-existing `Status.DriftDetected` bool (`types.go`), which just means
  "the manifest hash changed, additive updates were applied" — unrelated to `SchemaDrift`,
  which is what neither the auto-migrator nor a hash comparison can fix.
