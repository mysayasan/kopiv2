# Module: infra/db/bootstrap/bootstrap.go

## Purpose

Runs the shared database bootstrap process at application startup.

## Responsibilities

- Check whether the target database exists.
- Create the database when allowed.
- Open the target database and verify connectivity.
- Ensure the bootstrap state table exists.
- Create missing tables from the entity manifest.
- Add missing columns when safe additive migration is enabled.
- Reconcile unique indexes from entity tags.
- Persist the manifest hash and manifest JSON.
- Run seeders when enabled.

## Flow

1. Normalize bootstrap config.
2. Check/create database.
3. Open target DB.
4. Ensure bootstrap state table.
5. Build manifest from registered entities.
6. Compare stored manifest hash with the current one.
7. Apply additive schema updates.
8. Execute seeders if configured.
9. Persist the new manifest state.

## Safety Notes

- No destructive dropping is performed.
- Unsafe entity changes are not auto-applied.
- The engine is designed for startup bootstrap, not an interactive SQL console.
- Bootstrap currently supports `db.engine=postgres`, `db.engine=mariadb`, and `db.engine=sqlite`.
- SQLite `db_name` is treated as a database file path; `:memory:` is supported for tests/dev experiments.
- SQLite uses file existence for database existence checks and initializes the file with the same bootstrap state manifest flow.
