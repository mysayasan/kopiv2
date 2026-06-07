# Module: infra/coordination/redis.go

## Purpose

Implements Redis-backed FIFO transaction locking for multi-instance deployments.

## Responsibilities

- Enqueues lock waiters per resource.
- Acquires resource ownership with token-based Redis `SET NX` leases.
- Renews the owner lease while the process is active.
- Releases locks only when the stored owner token matches.
- Drops stale queue heads when their waiter heartbeat has expired and no active owner exists.
- Emits acquire, timeout/cancel, error, and stuck-lock telemetry.

## Notes

- Redis is the recommended provider for production deployments with more than one app process.
- Lease renewal reduces duplicate execution risk, while owner tokens prevent stale owners from deleting newer locks.
