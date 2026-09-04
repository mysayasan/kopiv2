# Module: infra/db/sql/postgres/db_crud_sel.go

## Purpose

Builds and executes PostgreSQL select queries for the runtime DB adapter.

## Responsibilities

- Generate list select SQL from reflected entity fields, filters, sorters, optional joins, `limit`, and `offset`.
- Add a window-count column when a result window is requested so callers receive `totalCnt` alongside the page data.
- Scan rows using destinations selected via `dbsql.ScanDestinationForField` (`infra/db/sql/scan_value.go.md`) and normalized back with `dbsql.NormalizeScannedValue` — every scannable Go kind (string, bool, and now every signed/unsigned int and float kind) goes through a `sql.NullXxx` destination, so a `NULL` in a column the auto-migrator added with no default reads as the field's zero value instead of failing the whole `SELECT` (`converting NULL to int64 is unsupported`). This driver's own `scanDestinationForField`/`normalizeScannedValue` (numeric-kind-unsafe, and duplicated with the mariadb driver's) are gone — both call into the shared `infra/db/sql/scan_value.go` now.
- Convert signed database row counts into safe unsigned totals.
- Return the current result count as `totalCnt` when no window-count column is present.
- `SelectByUnique` now fail-closes when the supplied key group matches no struct field: it returns `(nil, nil)` immediately rather than issuing an unfiltered `LIMIT 1` query that would silently return the first row (same fix as the SQLite adapter — prevents privilege-escalation via a missing `ukey` tag).
- `selectWithSQL` no longer hardcodes a 100-row scan ceiling (same fix as the SQLite and MariaDB adapters). `Select`/`SelectJoin` pass the caller's `limit` through, and the scan loop is capped at `dbsql.ScanRowLimit(limit)` — see `infra/db/sql/scan_limit.go.md` and `sqlite/db_crud_sel.go.md` for the full defect this closes across all three drivers.
