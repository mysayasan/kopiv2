# Module: infra/db/sql/postgres/db_crud_sel.go

## Purpose

Builds and executes PostgreSQL select queries for the runtime DB adapter.

## Responsibilities

- Generate list select SQL from reflected entity fields, filters, sorters, optional joins, `limit`, and `offset`.
- Add a window-count column when a result window is requested so callers receive `totalCnt` alongside the page data.
- Scan rows using destinations selected from reflected field types.
- Normalize nullable database strings into empty strings for plain Go string fields.
- Convert signed database row counts into safe unsigned totals.
- Return the current result count as `totalCnt` when no window-count column is present.
