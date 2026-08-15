# Module: infra/coordination/locker.go

## Purpose

Defines transaction coordination contracts shared by memory and Redis lock providers.

## Responsibilities

- Defines `Locker` for FIFO resource lock acquisition (`Lock`) and, new, single-attempt
  acquisition (`TryLock`).
  - `Lock(ctx, resource) (Lock, error)` — waits its turn in a FIFO queue and returns once held, or
    fails after `WaitTimeout`. For work that MUST eventually happen (the migration lock,
    transaction-coordinated file-storage work).
  - `TryLock(ctx, resource) (Lock, error)` — attempts once and returns `ErrNotAcquired` immediately
    if somebody else holds it; never queues. For work that must happen AT MOST ONCE across the
    deployment, where a loser should skip rather than wait — this is the primitive leader election
    (`leader.go.md`) is built on. `Lock` is deliberately unsuitable there: a loser would block for
    `WaitTimeout` and then acquire the lock the instant the winner released it, re-running the very
    job the lock existed to deduplicate.
- `ErrNotAcquired` — the sentinel `TryLock` returns when the resource is already held. An ordinary
  outcome (the caller asked "am I the one to do this?" and the answer was no), not a failure.
- Defines `Lock` for owner-token release (`Release`) and, new, liveness re-verification (`Valid`).
  - `Valid(ctx) bool` — reports whether this lock is STILL held by this holder. A lease can be lost
    silently — the process stalls past its TTL, or the backing store drops the key — so a long-lived
    holder (a leadership lease held for the process's whole lifetime) must re-check rather than
    assume, or it becomes a silent second writer. `MemoryLocker`'s implementation has no lease to
    expire (it can only stop holding via `Release`); `RedisLocker`'s re-reads the key from Redis and
    treats an unreachable Redis as NOT held — failing closed, because the alternative (assuming still
    held) would let every instance believe it is in charge during exactly the partition where that
    does the most damage.
- Defines `Ping` lifecycle checks so startup can fail fast when the configured provider is unavailable.
- Defines shared timing/config values for wait timeout, lease, stuck timeout, and Redis connection settings.
- Emits shared coordination telemetry observations through the telemetry recorder interface.

## Notes

- The coordinator serializes critical app-level work; it does not replace request-scoped DB transactions.
- Resource labels must stay low-cardinality for metrics.
- `TryLock`'s successful acquisition deliberately skips the stuck-lock monitor both providers run
  for `Lock`: a lease taken through `TryLock` (a leadership lease) may legitimately be held for the
  life of the process, and reporting that as "stuck" on every `StuckTimeout` would be a false alarm
  on a healthy deployment.
