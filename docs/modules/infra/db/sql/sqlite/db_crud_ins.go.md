# Module: infra/db/sql/sqlite/db_crud_ins.go

## Purpose

Builds and executes SQLite insert statements for the runtime DB adapter.

## Responsibilities

- Reflect persistent struct fields into insert columns.
- Omit fields tagged `skipWhenInsert:"true"`.
- Convert Go values into SQLite-safe SQL literals.
- Return SQLite `LastInsertId` as the created row ID.
- Support slice/array inserts by inserting each item sequentially.
