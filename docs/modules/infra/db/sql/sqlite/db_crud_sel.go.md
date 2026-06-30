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
