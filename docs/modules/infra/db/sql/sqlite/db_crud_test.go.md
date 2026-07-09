# Module: infra/db/sql/sqlite/db_crud_test.go

## Purpose

Validates the SQLite runtime adapter against the shared repository contract.

## Coverage

- Creates a temporary file-backed SQLite database.
- Exercises create, read, update, delete, filtering, sorting, and total-count behavior through `GenericRepo`.
- Verifies request-scoped transaction rollback behavior.
- Regression test for the `SelectByUnique` fail-closed fix: asserts that a `GetByUnique` call whose key group matches no field returns `(nil, nil)` rather than the first row, confirming the auth-escalation bug is closed.
- Verifies the `In` compare operator builds a correct `col IN (...)` clause (multi-value filter matches any listed value) and that an empty `In` list drops the constraint entirely rather than matching nothing.
