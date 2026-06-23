# Module: infra/db/bootstrap/reset.go

## Purpose

Performs a destructive, deliberate factory reset of the configured database: drop the whole database (honouring the engine and name), then re-run `Ensure()` to recreate the schema and re-seed stock data from a clean slate.

## Responsibilities

- `Reset(ctx, Options)` — gated by `Bootstrap.AllowReset` (errors when false so a stock deployment can't be wiped by accident); validates the engine, calls `dropDatabase`, then re-runs `Ensure()` with the create/migrate/seed flags forced on.
- `dropDatabase(ctx, cfg, engine)` — removes the configured database:
  - **postgres** — best-effort `pg_terminate_backend` of other sessions (errors ignored), then `DROP DATABASE IF EXISTS <db> WITH (FORCE)` (PG13+ evicts any remaining connection itself); falls back to a plain `DROP DATABASE` if the forced form is rejected by an older server.
  - **mariadb** — `DROP DATABASE IF EXISTS <db>`.
  - **sqlite** — deletes the database file plus its `-wal` / `-shm` / `-journal` sidecars.

## Notes

- The caller MUST stop everything holding a connection/file handle to the database first and restart the app afterwards — the live connection pool is invalid once the database is dropped. In `mymatasan` this is orchestrated by `services.SystemResetService` behind `POST /api/system/reset` and the `apphost.Restarter`.
- `WITH (FORCE)` makes the wipe unblockable on modern Postgres; the terminate step is belt-and-suspenders for older servers and is intentionally non-fatal.
- Reuses the package's existing helpers (`normalizeDbEngine`, `openMaintenanceDB`, `quoteIdent`, `Ensure`).
- Covered by `reset_test.go` (sqlite drop+rebuild wipes user rows; the `AllowReset=false` guard refuses and leaves the database intact).
