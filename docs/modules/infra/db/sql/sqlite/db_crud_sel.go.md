# Module: infra/db/sql/sqlite/db_crud_sel.go

## Purpose

Builds and executes SQLite select queries for the runtime DB adapter.

## Responsibilities

- Generate list select SQL from reflected entity fields, filters, sorters, optional joins, `limit`, and `offset`.
- Use a CTE and count subquery so callers receive `totalCnt` alongside the result page.
- Support reusable explicit join specs through `SelectJoin`.
- Scan rows into values compatible with the shared generic repository and mapstructure decoding.
- Normalize SQLite integer booleans, integer aliases, floats, byte slices, strings, and `sql.NullString`.
- Return an empty result through the shared no-result error convention used by `GenericRepo`.
- `SelectByUnique` now fail-closes when the supplied key group matches no struct field: it returns `(nil, nil)` immediately rather than issuing an unfiltered `LIMIT 1` query that would silently return the first row. This was a severe auth bug — every `GetByUnique(ctx, "", "id", id)` call on an entity whose `Id` field has no `ukey:"id"` tag resolved to the first DB row (the stock superadmin), making every user/role lookup resolve as superadmin.
- `selectWithSQL` no longer hardcodes a 100-row scan ceiling. `Select`/`SelectJoin` now pass the caller's `limit` through to `selectWithSQL`, which caps the scan loop at `dbsql.ScanRowLimit(limit)` (see `infra/db/sql/scan_limit.go.md`): the caller's own limit when it gave one (the generated SQL already bounds the result, so the scan just takes what came back), or `dbsql.DefaultScanRowLimit` (100) only when it asked for everything (`limit == 0`). Before this, a query with a real SQL `LIMIT` of e.g. 2000 still stopped scanning at row 100 while `x_rows_cnt` reported the true total from the database, so a truncated page was indistinguishable from a complete one. Pinned by `TestSQLiteSelectReturnsTheLimitAsked` (`db_crud_test.go.md`), which writes 250 rows and asserts a `limit=250` read returns all 250.
