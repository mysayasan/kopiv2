# Module: infra/db/sql/generic_repo.go

## Purpose

Typed repository implementation over `IDbCrud`.

## Behavior

- Converts DB rows (`map[string]interface{}`) into typed models via `mapstructure`.
- Provides generic CRUD and query helpers per entity type.
- Returns an empty list and zero total count for no-result list queries.
- Wraps DB errors with contextual messages using `%w`.
- `GetById` and `GetByUnique` return `(nil, nil)` — not a zero-value struct — when the underlying `SelectById`/`SelectByUnique` returns a nil map. This makes every `x == nil` not-found check across the codebase correct; previously a nil map decoded through `mapstructure` would produce a non-nil zero struct, silently defeating not-found guards in auth/RBAC lookups.

## Core Methods

- Read:
  - `Get`, `GetJoin`, `GetJoinWithSpec`, `GetSingle`, `GetById`, `GetByUnique`, `GetByForeign`
- Write:
  - `Create`, `CreateMultiple`
  - `UpdateById`, `UpdateByUnique`, `UpdateByForeign`
  - `Delete`, `DeleteById`, `DeleteByUnique`, `DeleteByForeign`

## Why It Matters

- Standardizes repository behavior across entities.
- Preserves root cause errors while adding operation context.
- Allows services to build reusable joined list projections with explicit join aliases through `JoinSpec`.
