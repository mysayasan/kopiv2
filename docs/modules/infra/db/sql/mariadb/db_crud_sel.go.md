# Module: infra/db/sql/mariadb/db_crud_sel.go

## Purpose

Builds and executes MariaDB select queries for the runtime DB adapter.

## Responsibilities

- Generate list select SQL from reflected entity fields, filters, sorters, optional joins, `limit`, and `offset`.
- Add MariaDB's maximum `LIMIT` value when callers request `OFFSET` without `LIMIT`.
- Scan rows using destinations selected via `dbsql.ScanDestinationForField` (`infra/db/sql/scan_value.go.md`) and normalized back with `dbsql.NormalizeScannedValue` — every scannable Go kind (string, bool, and now every signed/unsigned int and float kind) goes through a `sql.NullXxx` destination, so a `NULL` in a column the auto-migrator added with no default reads as the field's zero value instead of failing the whole `SELECT` (`converting NULL to int64 is unsupported`). This driver's own `scanDestinationForField`/`normalizeScannedValue` (numeric-kind-unsafe, and duplicated with the postgres driver's) are gone — both call into the shared `infra/db/sql/scan_value.go` now.
- Return selected rows plus the total count expected by repository callers.
- `selectWithSQL` no longer hardcodes a 100-row scan ceiling (same fix as the SQLite and Postgres adapters). `Select`/`SelectJoin` pass the caller's `limit` through, and the scan loop is capped at `dbsql.ScanRowLimit(limit)` — see `infra/db/sql/scan_limit.go.md` and `sqlite/db_crud_sel.go.md` for the full defect this closes across all three drivers.
