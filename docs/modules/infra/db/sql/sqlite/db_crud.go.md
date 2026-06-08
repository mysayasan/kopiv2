# Module: infra/db/sql/sqlite/db_crud.go

## Purpose

SQLite implementation of `IDbCrud`.

## Key Responsibilities

- Open a SQLite database from `db.db_name`.
- Create the parent database directory when using a file-backed database.
- Configure SQLite pragmas for foreign keys, WAL journal mode, and busy timeout.
- Limit the adapter to one open DB connection to avoid file-lock contention.
- Execute ping checks via `Ping(ctx)`.
- Handle transaction lifecycle, including request-scoped transaction handles through `BeginScopedTx`.
- Build SQL fragments for joins, filters, sorting, and columns.

## Connection Contract

- `db_name` is the SQLite database path.
- `:memory:` is supported for tests/dev experiments.
- Host, port, username, password, and SSL mode are ignored by the SQLite adapter.

## Operational Notes

- SQLite support is shared infra and can be selected by any app through `db.engine=sqlite`.
- SQLite is best suited to single-process small-device deployments.
- Use PostgreSQL or MariaDB for multi-instance production workloads.
