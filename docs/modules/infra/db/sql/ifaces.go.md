# Module: infra/db/sql/ifaces.go

## Purpose

Defines database abstraction contracts used across the application.

## Interfaces

- `IDbCrud`
  - low-level CRUD/select primitives
  - transaction controls (`BeginTx`, `RollbackTx`, `CommitTx`)
  - health primitive (`Ping(ctx)`)
- `ScopedTxStarter`
  - creates request-scoped transaction-bound CRUD handles
  - avoids storing active transaction state on shared runtime DB adapters
- `IGenericRepo[T]`
  - typed repository API for services
  - list/get/create/update/delete operations, including filtered delete for retention cleanup

## Design Intent

- Decouple services from SQL implementation details.
- Enable testing with fake or in-memory implementations.
- Keep repository and DB adapter boundaries explicit.
