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
- `TryLock(ctx, resource) (Lock, error)` — a single `SET NX` attempt with no FIFO queue at all: a
  caller that loses wants to skip this round, not be handed the lock later once the winner
  finishes. On success the returned lock's lease is renewed for as long as it is held (same
  renewal loop `Lock` uses), so a long-lived holder (a leadership lease) stays owner while it is
  alive, and a holder that dies has its lease expire within `LeaseTTL` and lets another instance
  take over. Deliberately skips `monitor()` (the stuck-lock telemetry) for the same reason
  `MemoryLocker.TryLock` does.
- `key(resource)` — builds the Redis key as `<keyPrefix>:<appName>:tx:<resource>`, omitting either
  segment that is empty. The **application name is part of the key** because sharing one Redis
  across the suite is the recommended arrangement — Redis is precisely what makes an app
  clusterable — and without it two apps' locks on the same resource name collided. The concrete
  case: myseliasan's leader lease and myidsan's leader lease were both `kopiv2:tx:lock:leader`, so
  only one of the two apps could hold leadership at a time and the loser silently ran none of its
  scheduled singletons for as long as the winner kept renewing.
- `redisLock.Valid(ctx) bool` — re-reads the lock key from Redis and compares it against this
  holder's own token; this is what keeps a lease honest when the renewal timer itself cannot be
  trusted (a long GC pause, a suspended VM, a network partition can all let `LeaseTTL` elapse
  before the next renewal fires, at which point the key may already belong to someone else). An
  **unreachable Redis is reported as NOT held** — failing closed. Failing open would let every
  instance decide it is the one in charge during exactly the partition where that does the most
  damage; failing closed costs at most one skipped round of work.

## Notes

- Redis is the recommended provider for production deployments with more than one app process.
- Lease renewal reduces duplicate execution risk, while owner tokens prevent stale owners from deleting newer locks.
- `TryLock`/`Valid` together are what `infra/coordination/leader.go` is built on: `Elect` calls
  `TryLock` on a retry timer to campaign, and calls `Valid` on the same timer, while already
  holding the lease, to confirm it has not been silently lost.
