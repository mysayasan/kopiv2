# Module: infra/db/sql/mariadb/db_crud.go

## Purpose

MariaDB implementation of `IDbCrud` for runtime repository operations.

## Key Responsibilities

- Open and validate MariaDB connection in `NewDbCrud`.
- Expose transaction lifecycle methods, including request-scoped transaction handles through `BeginScopedTx`.
- Expose `Ping(ctx)` for readiness checks.
- Reuse the existing SQL CRUD generation strategy used by shared repositories.

## Connection Contract

DSN uses:

- `user`
- `password`
- `host`
- `port`
- `db_name`
- query options: `parseTime=true`, `multiStatements=true`

## Notes

- This adapter enables `db.engine=mariadb` end-to-end runtime support.
- Bootstrap and seed flow now run against MariaDB with dialect-aware SQL in the bootstrap package.
- SQL filters support equality and range comparisons for list/update/delete operations.
- Filter values are formatted by reflected field kind so defined integer enum fields are treated as numeric values.
- String filter values escape single quotes before being embedded in generated SQL.
- Nullable database strings are scanned through `sql.NullString` and normalized to empty Go strings for string entity fields.
- Offset-only selects add MariaDB's maximum `LIMIT` value because MariaDB requires `LIMIT` before `OFFSET`.
- Scan destinations are derived from reflected field types so defined integer aliases, booleans, floats, byte slices, strings, and `sql.NullString` are handled consistently.
- New transactional service workflows should use scoped transaction handles rather than mutating transaction state on the shared adapter.
