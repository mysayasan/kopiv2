# Module: infra/db/bootstrap/schema.go

## Purpose

Builds the code-first manifest and table specifications from registered entity structs.

## Responsibilities

- Reflect over structs and ignore non-persistent fields.
- Convert Go field types into engine-specific column types.
- Derive table names from struct names.
- Collect `ukey` groups into unique index definitions.
- Generate a stable manifest hash for drift tracking.
- Preserve entity field order when generating table columns.

## Mapping Rules

- `pkey:"true"` marks the primary key.
- `skipWhenInsert:"true"` on a primary key enables auto-increment behavior.
- `validate:"required"` makes a field non-null during table creation.
- `ukey` tags are grouped into unique indexes.
- slice fields are ignored so embedded relations do not become columns.
- Reflected entity field order is the source of truth for `CREATE TABLE` column order.

## Notes

- Missing columns are treated as additive schema drift.
- The manifest hash is used as the bootstrap state fingerprint.
- SQLite maps integer-like types to `INTEGER`, booleans to `INTEGER`, floating types to `REAL`, and timestamp/JSON-like values to `TEXT`.
- SQLite auto-increment primary keys are emitted as `INTEGER PRIMARY KEY AUTOINCREMENT`.
- SQLite unique constraints are created through the shared unique-index reconciliation path instead of inline table constraints.
