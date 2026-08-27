# Module: infra/db/sql/mariadb/db_crud_sel.go

## Purpose

Builds and executes MariaDB select queries for the runtime DB adapter.

## Responsibilities

- Generate list select SQL from reflected entity fields, filters, sorters, optional joins, `limit`, and `offset`.
- Add MariaDB's maximum `LIMIT` value when callers request `OFFSET` without `LIMIT`.
- Scan rows using destinations selected from reflected field types.
- Normalize nullable database strings into empty strings for plain Go string fields.
- Return selected rows plus the total count expected by repository callers.
- `selectWithSQL` no longer hardcodes a 100-row scan ceiling (same fix as the SQLite and Postgres adapters). `Select`/`SelectJoin` pass the caller's `limit` through, and the scan loop is capped at `dbsql.ScanRowLimit(limit)` — see `infra/db/sql/scan_limit.go.md` and `sqlite/db_crud_sel.go.md` for the full defect this closes across all three drivers.
