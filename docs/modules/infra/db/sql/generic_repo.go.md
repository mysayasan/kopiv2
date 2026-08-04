# Module: infra/db/sql/generic_repo.go

## Purpose

Typed repository implementation over `IDbCrud`.

## Behavior

- Converts DB rows (`map[string]interface{}`) into typed models via `mapstructure`.
- Provides generic CRUD and query helpers per entity type.
- Returns an empty list and zero total count for no-result list queries.
- Wraps DB errors with contextual messages using `%w`.
- `GetByUnique` returns `(nil, nil)` — not a zero-value struct — when the underlying `SelectByUnique` returns a nil map. This makes every `x == nil` not-found check for it correct; previously a nil map decoded through `mapstructure` would produce a non-nil zero struct, silently defeating not-found guards in auth/RBAC lookups.
- `GetById` does **not** have the same behavior, despite an in-code comment on the `res == nil` branch that suggests it does. `SelectById`'s underlying `Select` treats an empty result set as an *error* (`"no result found"`), not a nil map, so a missing row surfaces from `GetById` as a wrapped error (`"select by id failed: ..."`), and the `res == nil` check is dead code for the not-found case. `Get`, by contrast, swallows that same error via `isNoResultErr` and returns an empty slice. Callers that need "missing row → nil, no error" out of `GetById` must match the error string themselves (e.g. `apps/mypintusan/services/store_sql.go`'s `isNotFound`) — there is no typed sentinel or exported helper for it today.

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
