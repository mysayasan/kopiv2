# Module: infra/db/bootstrap/sqlite_test.go

## Purpose

Validates SQLite support in the shared bootstrap engine.

## Coverage

- Creates a temporary file-backed SQLite database through `bootstrap.Ensure`.
- Verifies table creation from reflected entities.
- Verifies unique-index reconciliation.
- Verifies `idx:"group"` non-unique composite secondary-index reconciliation: a two-field `idx` group creates one index (`ix_<table>_<group>`) spanning both columns in field-declaration order.
- Verifies bootstrap manifest state persistence.
- Verifies idempotent second-run behavior.
