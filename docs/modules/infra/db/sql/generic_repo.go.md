# Module: infra/db/sql/generic_repo.go

## Purpose

Typed repository implementation over `IDbCrud`.

## Behavior

- Converts DB rows (`map[string]interface{}`) into typed models via `mapstructure`.
- Provides generic CRUD and query helpers per entity type.
- Returns an empty list and zero total count for no-result list queries.
- Wraps DB errors with contextual messages using `%w`.

## Core Methods

- Read:
  - `Get`, `GetJoin`, `GetSingle`, `GetById`, `GetByUnique`, `GetByForeign`
- Write:
  - `Create`, `CreateMultiple`
  - `UpdateById`, `UpdateByUnique`, `UpdateByForeign`
  - `Delete`, `DeleteById`, `DeleteByUnique`, `DeleteByForeign`

## Why It Matters

- Standardizes repository behavior across entities.
- Preserves root cause errors while adding operation context.
