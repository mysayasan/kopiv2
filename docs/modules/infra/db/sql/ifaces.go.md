# Module: infra/db/sql/ifaces.go

## Purpose

Defines database abstraction contracts used across the application.

## Interfaces

- `IDbCrud`
  - low-level CRUD/select primitives
  - `SelectJoin` accepts reusable `JoinSpec` entries when callers need explicit join aliases for projection models
  - transaction controls (`BeginTx`, `RollbackTx`, `CommitTx`)
  - health primitive (`Ping(ctx)`)
- `ScopedTxStarter`
  - creates request-scoped transaction-bound CRUD handles
  - avoids storing active transaction state on shared runtime DB adapters
- `IGenericRepo[T]`
  - typed repository API for services
  - `GetJoinWithSpec` exposes explicit join specs while preserving the older `GetJoin` string-source helper
  - list/get/create/update/delete operations, including filtered delete for retention cleanup

## Design Intent

- Decouple services from SQL implementation details.
- Enable testing with fake or in-memory implementations.
- Keep repository and DB adapter boundaries explicit.
