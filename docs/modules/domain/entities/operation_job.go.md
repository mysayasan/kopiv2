# Module: domain/entities/operation_job.go

## Purpose

Defines the durable operation job entity used by backend workers.

## Responsibilities

- Store operation type and resource key for worker routing.
- Store an idempotency key with a unique index tag.
- Track lifecycle status, attempts, worker lock token, deadlines, completion time, and last error.
- Store encoded payload and result data for async operation recovery and status polling.

## Notes

- File-storage async upload jobs use this entity to survive request completion and worker restarts.
- `CreatedAt` is insert-only; `UpdatedAt`, `StartedAt`, `DeadlineAt`, and `CompletedAt` are maintained by the worker.
