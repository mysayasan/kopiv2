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
