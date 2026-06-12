# Module: infra/db/sql/sqlite/db_crud_del.go

## Purpose

Builds and executes SQLite delete statements for the runtime DB adapter.

## Responsibilities

- Require explicit filters before deleting rows.
- Support primary, unique, and foreign-key tag based deletes.
- Return the number of affected rows and fail when no row was deleted.
