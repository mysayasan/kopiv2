# Module: infra/db/sql/sqlite/db_crud_test.go

## Purpose

Validates the SQLite runtime adapter against the shared repository contract.

## Coverage

- Creates a temporary file-backed SQLite database.
- Exercises create, read, update, delete, filtering, sorting, and total-count behavior through `GenericRepo`.
- Verifies request-scoped transaction rollback behavior.
- Regression test for the `SelectByUnique` fail-closed fix: asserts that a `GetByUnique` call whose key group matches no field returns `(nil, nil)` rather than the first row, confirming the auth-escalation bug is closed.
