# Module: infra/db/sql/sqlite/db_crud_test.go

## Purpose

Validates the SQLite runtime adapter against the shared repository contract.

## Coverage

- Creates a temporary file-backed SQLite database.
- Exercises create, read, update, delete, filtering, sorting, and total-count behavior through `GenericRepo`.
- Verifies request-scoped transaction rollback behavior.
