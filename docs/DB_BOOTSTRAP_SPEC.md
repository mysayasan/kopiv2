# DB Bootstrap Specification

## Goal

Provide one shared, code-first database bootstrap system that can be reused by new apps without creating a custom setup service for each app.

The app should only register its entity types and optional seed providers. The shared bootstrap engine should handle database existence checks, schema creation, and initial data seeding.

## Core Principle

Entities are the source of truth.

The bootstrap engine reflects over app entity structs and uses metadata tags to build tables and constraints.

## Standard Responsibilities

### Shared bootstrap engine in infra

The shared engine should own:

- database existence checks
- schema creation from entity metadata
- table creation
- primary key generation
- index generation
- foreign key creation where metadata is available
- seed execution
- bootstrap status reporting
- idempotent setup execution

### App layer responsibilities

Each app should only provide:

- entity registration
- seed registration
- config flags for bootstrap behavior
- optional app name or namespace for setup UI and logging

The app should not implement its own bootstrap service unless it needs custom behavior beyond the shared standard.

## Proposed Folder Shape

Suggested layout:

- `infra/db/bootstrap`
  - shared bootstrap engine
  - entity scanner
  - schema builder
  - seed runner
  - setup status APIs
- `apps/<app>/entities`
  - schema source of truth
- `apps/<app>/config.json`
  - bootstrap and seeding flags
- `apps/<app>/main.go`
  - registers entities with the shared bootstrap engine

## Startup Flow

1. App loads config.
2. App builds entity registry.
3. Shared bootstrap engine checks database connectivity.
4. If database is missing and auto-create is enabled, the engine creates it.
5. Engine opens the target database and ensures the bootstrap state table exists.
6. Engine runs versioned structural migrations (see [Versioned Migrations](#versioned-migrations-tier-2-phase-m), below) — before schema creation/auto-migrate.
7. Engine creates schema from entities and adds missing columns when safe additive migration is enabled.
8. Engine detects and reports schema drift the additive path cannot fix (see [Schema Drift Detection](#schema-drift-detection), below).
9. Engine seeds initial rows if enabled.
10. Engine stores the applied manifest hash.
11. App transitions into normal runtime mode.

## Bootstrap Modes

### Dev mode

Recommended defaults:

- auto-create database: true
- auto-create schema: true
- auto-seed: true
- auto-migrate: true

### Production mode

Recommended defaults:

- auto-create database: false
- auto-create schema: false or controlled
- auto-migrate: controlled
- auto-seed: false unless explicitly enabled

Versioned structural migrations (see [Versioned Migrations](#versioned-migrations-tier-2-phase-m)) are **not** gated by `autoMigrate` — they run whenever `bootstrap.enabled` is true, since they are how an app-registered rename/drop/type-change reaches a production database at all. There is currently no separate flag to disable them independently of bootstrap itself.

## Entity Metadata Contract

Entities should carry explicit tags that describe database behavior.

Recommended tags:

- table name
- column name
- primary key
- unique key
- nullable
- default value
- index hint
- foreign key hint
- seed hint when needed

Two of these are implemented as concrete struct tags read by `infra/db/bootstrap/schema.go`: `ukey:"group"` groups fields into a **unique** index, `idx:"group"` groups fields into a **non-unique** secondary index (composite when more than one field shares the group name, column order = field declaration order). A field may join multiple `idx` groups by listing them comma-separated (`idx:"cam_time, time"`) — useful for a trailing timestamp column that is both the last column of a scoped composite index and the sole column of the plain index a retention purge scans. Both are reconciled the same way on every bootstrap run (`ensureIndexes` in `infra/db/bootstrap/bootstrap.go`, `CREATE [UNIQUE] INDEX IF NOT EXISTS`), so adding an `idx` tag to an existing entity is a safe additive migration — no hand-written seeder needed for a plain composite index.

Example intent:

- struct field names remain Go-friendly
- tags describe DB behavior
- reflection reads tags during bootstrap

## Seed Contract

Seeding should be separate from schema generation.

Recommended seed model:

- register named seed providers per app
- each provider returns rows to insert
- seed execution is idempotent where possible
- seed profiles can be enabled by config

## Setup Page Behavior

The setup page should be a thin UI over server-side provisioning.

It should show:

- DB reachability status
- database existence status
- schema readiness status
- seed status
- provisioning action button

It should not execute SQL directly from the browser.

## Recommended HTTP Surface

Shared endpoints can be standardized as:

- `GET /setup/status`
- `POST /setup/provision`
- `POST /setup/seed`
- `POST /setup/reset` only when explicitly allowed in dev

Current implementation also exposes:

- `GET <setupPath>` for the bootstrap status page
- `GET <setupPath>/status` for the JSON status payload

The app can redirect to the setup page when bootstrap mode is active.

## Safety Rules

- Never auto-drop production databases.
- Never auto-reset schema unless a dev-only flag permits it.
- Never expose raw SQL execution from the browser.
- Never infer schema from entities without an explicit registry boundary.

## Factory Reset (`bootstrap.Reset`)

`Reset(ctx, Options)` performs a destructive, deliberate factory reset, gated by `allowReset` (returns an error when it is false, so a stock deployment cannot be wiped by accident). It:

1. **Drops the whole database**, honouring the configured engine:
   - **postgres** — best-effort `pg_terminate_backend` of other sessions (errors ignored), then `DROP DATABASE IF EXISTS <db> WITH (FORCE)` so any remaining connection is evicted (PG13+); falls back to a plain `DROP DATABASE` for older servers.
   - **mariadb** — `DROP DATABASE IF EXISTS <db>`.
   - **sqlite** — deletes the database file and its `-wal` / `-shm` / `-journal` sidecars, via `removeWithRetry` (a few short retries on a lingering Windows "file in use" error, since the OS can take a moment to release handles after the caller closes its connection pool).
2. **Re-runs `Ensure()`** with the create/migrate/seed flags forced on, recreating the schema and re-seeding stock data from a clean slate.

The caller stops anything holding a connection/file handle first and restarts the process afterwards (the live connection pool is invalid once the database is dropped). In `mymatasan` this is orchestrated by `apps/mymatasan/services.SystemResetService` (shred media → close its own DB pool via `SystemResetConfig.CloseDatabase`, now implemented by all three `IDbCrud` engines — required on sqlite/Windows, where the file can't otherwise be deleted while the process holds it open → `Reset` → restart via `apphost.Restarter`) behind `POST /api/system/reset`; a `ResetGate` middleware 503s other requests while `InProgress()`, since the DB pool is already closed at that point. The reset is best-effort: a wipe error is reported as a warning and the process still restarts, which re-runs bootstrap and can complete an interrupted rebuild.

`myseliasan`, `myidsan`, and `myiotsan` — which previously had no factory reset at all — share a simpler sibling orchestrator instead of each growing its own: `domain/shared/services.SystemResetService` (`docs/modules/domain/shared/services/system_reset.go.md`), with `domain/shared/apis.NewResetGate`/`NewSystemResetHandlers` (`docs/modules/domain/shared/apis/system_reset.go.md`) providing the same `POST /api/system/reset` + `GET .../state` + `GET .../progress` contract and the same "503 the rest, serve progress itself" gate. It omits mymatasan's shred/TRIM/free-space-scrub stages — none of these three apps holds a large media library — but drives through the identical `bootstrap.Reset` call, so the baselining trap below applies to it exactly the same way. `ResetBootstrapOptions` is the helper that builds each app's `bootstrap.Options` for the reset call site.

## Versioned Migrations (Tier 2 phase M)

The additive path above (`ADD COLUMN`, create missing tables/indexes) is the entire
migration engine that existed before this. That is fine while a schema only grows, and had
no answer the first time it didn't — on a field appliance nobody can reach:

- a **rename** added the new column and silently left all the data stranded in the old one
- a **drop** left the column forever — a legacy `NOT NULL` column with no default eventually
  breaks inserts
- a **type change** was not detected at all — only column *names* were ever compared, so the
  row scanner failed at runtime on a customer's box with no clue why

`infra/db/bootstrap/migration.go` adds a versioned, once-only migration mechanism that runs
**alongside**, not instead of, the additive auto-migrate path — additive changes still need
no migration; add a field to the entity and auto-migrate picks it up. A migration is for what
additive cannot express: a rename, a drop, a type change, or a data transform.

### The pipeline (order is load-bearing)

```
migrations (structural)  ->  auto-migrate (additive)  ->  drift check  ->  seeders (data)
```

Migrations run **before** the additive auto-migrator. That is the only order that works: a
rename has to happen while the column still has its old name. If auto-migrate went first it
would see the entity's new field, `ADD` it as an empty column, and the rename would then fail
against a target that already exists — with the data still stranded in the old column.

### The `Migration` type

```go
type Migration struct {
    ID         string                                            // "20260714-01-rename-camera-host"
    Name       string                                             // human-readable, for the log
    Statements map[string][]string                                // keyed by engine
    Exec       func(ctx context.Context, tx *sql.Tx, engine string) error
}
```

- `ID` is the immutable, sortable identity. Migrations run in **ID order** — IDs are
  date-prefixed, so lexical order is chronological. **Never change the ID or the SQL of a
  released migration** — the bootstrapper checksums it and refuses to start if it was edited
  after being applied (see Checksum tamper detection, below).
- `Statements` is keyed by engine: the `""` key applies to every engine, and an
  engine-specific key (`"sqlite"` / `"postgres"` / `"mariadb"`) overrides it for that engine.
  The engines genuinely differ (SQLite could not `DROP COLUMN` before 3.35; none of the three
  agree on how to change a column's type), so a structural migration usually has to say what
  it means per engine rather than pretend they are the same database.
- `Exec`, when set, runs instead of `Statements` — for a change SQL alone cannot express
  (read rows, transform them, write them back).

`schema_migration` (`app_name, id, name, checksum, applied_at`, primary key
`(app_name, id)`) records which migrations have run, per app.

### Checksum tamper detection

An already-applied migration is fingerprinted (SHA-256 over its trimmed per-engine
statements) at the time it runs, and the fingerprint is stored alongside it. On every later
boot, the stored checksum is compared against what the current code says the migration
should be. If they differ, startup fails loudly: *"migration \<id\> was modified after it was
applied ... never edit a released migration — add a new one."* Two databases that both claim
to have run `"20260714-01"` must have had the same thing done to them; if the code says
otherwise, the schema on disk is not what the developer thinks it is.

A migration with an `Exec` function has no stable checksum (a Go closure cannot be hashed) —
it is recorded with an empty checksum and skipped by the tamper check.

### Baselining

A **fresh** database — one with no prior bootstrap-state row for this app — records every
migration as applied **without running it**. The schema was just created directly from the
current entities by auto-create, so it is already at the latest shape: replaying a rename
against a column that never had the old name would simply fail, and replaying a drop against
a column that was never created would too. The migrations describe how to bring an *old*
database to the current shape; a new one is already there.

This applies identically to the **factory reset** (`bootstrap.Reset`): the reset drops and
rebuilds the database, so the rebuilt one is fresh and must baseline, not replay. Omitting
`Migrations` from the `bootstrap.Options` passed to the reset path is a real trap — the
rebuild still saves a manifest (so the very next boot reads as "not fresh"), and with an
empty migration table on record, every migration would then be replayed against a brand-new
schema and fail: **a factory reset would leave the app unable to start.** `mymatasan`'s
reset call site (`apps/mymatasan/app/app.go`) passes `Migrations: m.Migrations()` for exactly
this reason; `myseliasan`'s does the same via `sharedservices.ResetBootstrapOptions(...,
m.Migrations(), ...)`. `myidsan` and `myiotsan` genuinely have no migrations to baseline
(neither module declares any), so their reset call sites correctly pass `nil` — an
intentional absence, not an oversight.

### Atomicity and the MariaDB caveat

Each migration runs in a transaction. Where the engine supports transactional DDL (SQLite,
Postgres), the schema statements and the `schema_migration` record land together, so a
failure leaves nothing half-done: no partial schema change, and no record claiming success.

**MariaDB implicitly commits each DDL statement**, so a migration there **cannot** be atomic
— a mid-way failure leaves the earlier statements applied but the migration unrecorded (so it
is retried on the next boot, re-running the already-applied earlier statements). Write
MariaDB migration statements defensively: `IF EXISTS` / `IF NOT EXISTS` on every statement,
so a retry after a partial failure is safe to re-run.

### How to write a migration

1. Pick an `ID` that sorts after every existing migration for this app:
   `YYYYMMDD-NN-short-description` (e.g. `20260715-01-rename-camera-host`).
2. Write `Statements` per engine. Start with the `""` (all-engines) key; add
   `"sqlite"`/`"postgres"`/`"mariadb"` overrides only where the engines actually diverge
   (renames and type changes usually need at least a MariaDB override — see
   `infra/db/bootstrap/migration_test.go`'s `renameCodeToSlug` for a worked example). Make
   MariaDB statements idempotent (`IF EXISTS`/`IF NOT EXISTS`) per the caveat above.
3. If the change needs data movement or transformation SQL can't express, set `Exec` instead
   of `Statements` and do it in Go against the given `*sql.Tx`.
4. Add the `Migration` to the app's `Migrator.Migrations()` slice (e.g.
   `apps/mymatasan/app/app.go`'s `(*module) Migrations()`), appended — never edited in place
   once released.
5. Test it against a real SQLite database the way `migration_test.go` does: seed a database in
   the *old* shape (including a bootstrap-state row so it reads as "not fresh"), run
   `bootstrap.Ensure`, and assert the data survived and the old shape is gone.
6. Never change the `ID` or the `Statements`/`Exec` of a migration after it has shipped in a
   release — add a new migration instead. The checksum check exists specifically to catch
   this mistake.
7. Additive-only changes (a new field) need **no** migration at all — the auto-migrator adds
   the column. Only reach for a migration when auto-migrate genuinely cannot do the job.

## Schema Drift Detection

`infra/db/bootstrap/drift.go` reports the differences the additive auto-migrator cannot fix,
on every boot against a non-fresh database, **after** auto-migrate has had its chance to add
any genuinely missing column. It never repairs anything — rewriting a column's type in place
is destructive and engine-specific, which is what a Migration is for.

Two kinds are detected:

- **`type_changed`** — the column exists but its type no longer matches what the entity
  declares. Previously not detected at all (only column *names* were ever compared); the row
  scanner would fail at runtime with no explanation.
- **`extra_column`** — the database has a column the entity no longer declares: a field that
  was renamed or removed without a migration. Its data is still there, untouched.

A column present on the entity but missing from the database is **not** drift — that's the
additive auto-migrator's job, and it adds it.

### Comparing the type the engine actually stores, not the manifest's neutral type

The comparison uses `typeFamily(normalizeSQLType(column.SQLType, engine))` — the type this
specific engine **actually gets** when the column is created — against the type the database
reports back, **not** the manifest's engine-neutral type. This distinction matters: SQLite
stores a `BOOLEAN` column as `INTEGER`, and MariaDB stores it as `TINYINT(1)`. Comparing the
raw manifest type against those would report false drift on every bool column, on every boot,
on two of the three engines. A warning that always fires is a warning nobody reads.
`typeFamily` further normalizes to a broad class (`int`, `text`, `bool`, `float`, `time`,
`blob`, `json`) so cosmetic differences (`TEXT` vs `VARCHAR(255)`, `BIGINT` vs `int8`) never
register as drift either — only the kind of change that would actually break a row scan does.

Auto-increment primary keys are skipped: `columnDefinition` rewrites them wholesale
(`BIGSERIAL` / `INTEGER PRIMARY KEY AUTOINCREMENT` / `BIGINT AUTO_INCREMENT`), so their
declared type says nothing useful about what's actually on disk.

Results are surfaced in `Status.SchemaDrift` (`[]SchemaDrift`), folded into `Status.Message`
when non-empty, and logged loudly by both `bootstrap.go` and `infra/apphost/run.go`. This is
distinct from the pre-existing `Status.DriftDetected` bool, which only means the manifest
hash changed and additive updates were applied — unrelated to `SchemaDrift`, which is what
neither the auto-migrator nor a hash comparison can fix on its own.

See `docs/modules/infra/db/bootstrap/migration.go.md` and `drift.go.md` for the full
implementation writeup.

## Config Proposal

Suggested config keys in `config.json`:

- `db.bootstrap.enabled`
- `db.bootstrap.autoCreateDatabase`
- `db.bootstrap.autoCreateSchema`
- `db.bootstrap.autoSeed`
- `db.bootstrap.allowReset`
- `db.bootstrap.seedProfile`
- `db.bootstrap.setupPath`
- `db.bootstrap.seedStatements`

## Minimal App Integration Contract

Every new app should only need to do this:

1. Import the shared bootstrap package.
2. Register entity types.
3. Register optional seed providers.
4. Pass bootstrap config.
5. Start server.

That is the standard I recommend for reuse across new apps.

`myidsan` follows this contract as an identity app: it registers identity, app registry, user session, endpoint, RBAC, logging, cache, file-storage, operation-job, and the shared `RuntimeSetting` (first-run setup-wizard completion flag) entities, then seeds its own identity-management endpoint catalog through app-local seeders.

## Recommended Next Implementation Step

Build the shared bootstrap engine first, then add a thin setup API and setup page on top of it.

## Current Implementation Note

The first implementation in this repository uses startup bootstrap plus additive schema reconciliation. It does not drop tables or columns automatically. Since Tier 2 phase M, an app can additionally declare versioned migrations (see [Versioned Migrations](#versioned-migrations-tier-2-phase-m)) for the structural changes additive reconciliation cannot express — those still only run what the app explicitly registers, never an automatic drop.

Bootstrap supports `db.engine=postgres`, `db.engine=mariadb`, and `db.engine=sqlite`. SQLite uses `db_name` as the database file path and creates the parent directory when `autoCreateDatabase` is enabled. Relative SQLite paths are resolved by apphost from the selected app directory before bootstrap runs.

SQLite follows the same manifest, table, additive-column migration, unique-index, and seeding flow as the server databases, but it is intended for single-process or small-device deployments. Use PostgreSQL or MariaDB when the app is deployed with multiple instances or needs server-database operational controls.

When `autoSeed` is enabled, the engine can execute config-defined SQL seed statements through the shared seeder helper.

The current apps also seed a minimal core identity dataset on first run:

- a `system` user group
- a `superadmin` user role associated with that group
- a default `superadmin` login account (`superadmin` / `superadmin123`, stored as bcrypt) linked to that role
- `app_registry` rows for `myidsan` and `mymatasan`
- `app_auth_config` and `app_redirect_uri` rows for registered browser relying apps such as `myseliasan`
- `user_session` table for SSO session audit/revocation storage; live session validation currently uses the configured cache provider
- wildcard-host `api_endpoint` rows with `appCode`, `accessTier`, and menu `metadata` plus `api_endpoint_rbac` rows for protected modules, so the default access rules and MyIDSan navigation are portable across hosts

`mymatasan` seeds only standalone endpoint metadata for health/version, ONVIF, settings, local-user, and vision routes. It no longer seeds identity or RBAC rows because app-local routes use DB-backed local Basic Auth. Its `detection_rule` entity is the source of truth for AI rule fields, including the additive `ruleConfig` JSON column used by line-crossing rules. `myidsan` seeds identity, user-management, app-registry, app-auth-config, app-redirect-uri, endpoint, endpoint-RBAC, cache, log, file-storage administration, SSO fallback endpoints, browser federated-auth endpoints, and selected relying-app policies for relying apps such as `myseliasan`.

`myseliasan` seeds only its lightweight local operational endpoint catalog and avoids registering user-management tables. It relies on MyIDSan for identity and receives users through the authorization-code callback flow.

Fresh schema bootstrap treats `api_endpoint` uniqueness as app-aware through `appCode + host + path`. Existing databases that previously created a host/path-only unique index may need a manual operator migration before they can store duplicate paths for multiple app codes.
