# Module: infra/db/sql/sqlite/db_crud_upd.go

## Purpose

Builds and executes SQLite update statements for the runtime DB adapter.

## Responsibilities

- Reflect persistent struct fields into `SET` assignments.
- Ignore primary key fields and fields tagged `ignoreOnUpdate:"true"`.
- Support primary, unique, and foreign-key tag based updates.
- Return the number of affected rows and fail when no row was updated.
