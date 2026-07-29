# Module: infra/db/sql/mariadb/db_crud.go

## Purpose

MariaDB implementation of `IDbCrud` for runtime repository operations.

## Key Responsibilities

- Open and validate MariaDB connection in `NewDbCrud`.
- Apply the connection-pool budget via `dbsql.ApplyPool(db, config.Pool)` right after `sql.Open`, before `Ping` — see `config_models.go` (`dbsql.DbConfigModel.Pool`, `dbsql.ApplyPool`). An absent/zero `db.pool` config yields a bounded pool (25 max open, 5 idle, 30-minute lifetime), not `database/sql`'s unlimited default, so a traffic burst cannot exhaust the connection budget of a database server shared with other applications.
- Expose transaction lifecycle methods, including request-scoped transaction handles through `BeginScopedTx`.
- Expose `Ping(ctx)` for readiness checks.
- Reuse the existing SQL CRUD generation strategy used by shared repositories.
- `Close() error` releases the underlying connection pool (`nil` if never opened) so a factory reset's `DROP DATABASE` isn't blocked by this app's own sessions. Safe to call once at shutdown/reset; the pool is unusable afterwards.

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
- SQL filters support equality and range comparisons for list/update/delete operations, plus a multi-value `IN (...)` clause (`inSqlClause`) for the `sqldataenums.In` compare operator (empty/non-slice value drops the filter).
- Joined list projections can use explicit `JoinSpec` aliases and field-level `dbcol` tags when selected DTO field names differ from source table columns.
- Filter values are formatted by reflected field kind so defined integer enum fields are treated as numeric values.
- String filter values escape single quotes before being embedded in generated SQL.
- Nullable database strings are scanned through `sql.NullString` and normalized to empty Go strings for string entity fields.
- Offset-only selects add MariaDB's maximum `LIMIT` value because MariaDB requires `LIMIT` before `OFFSET`.
- Scan destinations are derived from reflected field types so defined integer aliases, booleans, floats, byte slices, strings, and `sql.NullString` are handled consistently.
- New transactional service workflows should use scoped transaction handles rather than mutating transaction state on the shared adapter.
