# Module: infra/db/bootstrap/sqlite_test.go

## Purpose

Validates SQLite support in the shared bootstrap engine.

## Coverage

- Creates a temporary file-backed SQLite database through `bootstrap.Ensure`.
- Verifies table creation from reflected entities.
- Verifies unique-index reconciliation.
- Verifies bootstrap manifest state persistence.
- Verifies idempotent second-run behavior.
