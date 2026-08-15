# Module: infra/coordination/memory.go

## Purpose

Implements process-local FIFO transaction locking for development and tests.

## Responsibilities

- Maintains per-resource FIFO wait queues in memory.
- Allows one owner per resource until release.
- Removes canceled or timed-out waiters from the queue.
- Emits acquire, timeout/cancel, and stuck-lock telemetry.
- `TryLock(ctx, resource) (Lock, error)` — takes the resource immediately if `queue.owner == ""`,
  else returns `ErrNotAcquired` with no queueing at all. Emits an `"acquired"`/`"not-acquired"`
  telemetry observation like `Lock`, but deliberately skips the stuck-lock `monitor()` goroutine —
  a lock taken this way (a leadership lease) may be held for the process's whole lifetime by
  design.
- `memoryLock.Valid(ctx) bool` — reports whether this holder is still the resource's owner. There
  is no lease to expire and no external store to lose the key in a process-local map, so the only
  way to stop holding it is `Release`; `Valid` is effectively "was I released or superseded".

## Notes

- This provider is not safe for multi-instance production because it cannot coordinate across processes.
- With this provider, `TryLock` always succeeds for the first (and only) caller in the process —
  which is the whole mechanism behind single-instance leader election (`leader.go.md`) needing no
  provider-specific branch: nobody else is running, so the lone process always wins and behaves
  exactly as it did before leader election existed.
