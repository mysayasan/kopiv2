# Module: infra/db/sql/postgres/db_crud.go

## Purpose

PostgreSQL implementation of `IDbCrud`.

## Key Responsibilities

- Create and validate DB connection in `NewDbCrud`.
- Execute ping checks via `Ping(ctx)`.
- Handle transaction lifecycle, including request-scoped transaction handles through `BeginScopedTx`.
- Build SQL fragments for joins, filters, sorting, and columns.

## Connection Contract

DSN components used:

- host
- port
- user
- password
- dbname
- sslmode

## SQL Builder Notes

- Uses struct reflection and tags for mapping.
- Supports key-based filtering (`pkey`, `ukey`, `fkey`).
- Supports reusable explicit join specs through `SelectJoin`, including field-level `dbcol` tags for joined projection columns whose DTO names differ from database column names.
- Generates where/sort expressions from enum-based filter/sorter inputs, including equality and range comparisons.
- Formats filter values by reflected field kind so defined integer enum fields are treated as numeric values.
- Escapes single quotes in string filter values before embedding them in generated SQL.
- Scans nullable database strings through `sql.NullString` and normalizes NULL values to empty Go strings for string entity fields.
- List selects with `LIMIT` and/or `OFFSET` include a window count column so repositories can return `totalCnt` with the current result window.
- Scan destinations are derived from reflected field types so defined integer aliases, booleans, floats, byte slices, strings, and `sql.NullString` are handled consistently.

## Operational Notes

- Startup calls `db.Ping()` during initialization to fail fast.
- Readiness checks call `PingContext` at runtime.
- New transactional service workflows should use scoped transaction handles rather than mutating transaction state on the shared adapter.
