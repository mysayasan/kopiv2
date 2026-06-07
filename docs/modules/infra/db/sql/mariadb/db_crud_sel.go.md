# Module: infra/db/sql/mariadb/db_crud_sel.go

## Purpose

Builds and executes MariaDB select queries for the runtime DB adapter.

## Responsibilities

- Generate list select SQL from reflected entity fields, filters, sorters, optional joins, `limit`, and `offset`.
- Add MariaDB's maximum `LIMIT` value when callers request `OFFSET` without `LIMIT`.
- Scan rows using destinations selected from reflected field types.
- Normalize nullable database strings into empty strings for plain Go string fields.
- Return selected rows plus the total count expected by repository callers.
