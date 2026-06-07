# Module: infra/db/sql/mariadb/db_crud.go

## Purpose

MariaDB implementation of `IDbCrud` for runtime repository operations.

## Key Responsibilities

- Open and validate MariaDB connection in `NewDbCrud`.
- Expose transaction lifecycle methods.
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
- Nullable database strings are scanned through `sql.NullString` and normalized to empty Go strings for string entity fields.
