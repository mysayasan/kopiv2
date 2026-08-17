# Module: infra/coordination/leader.go

## Purpose

Answers one question for background work: "am I the instance that should do this?" Almost
everything an app schedules — retention purges, the notification rollup, the daily/weekly digest,
heartbeat reconciliation — must happen ONCE for the deployment, not once per process. Standalone,
that distinction is invisible and free to ignore. Behind a load balancer it is the difference
between a rollup that counts an event once and one that counts it N times, or one digest each
morning versus one per replica.

## Design: a lease, not a queue

Built on `Locker.TryLock`/`Lock.Valid` (`locker.go.md`) rather than the FIFO `Locker.Lock`. A
losing instance must get on with NOT doing the work — queueing would hand it the lock the moment
the leader finishes and it would then run exactly the job the lease was meant to deduplicate, only
late instead of concurrently. With the memory provider (standalone default) the single process
always wins immediately, so standalone behavior is unchanged and no caller needs a provider-specific
branch.

## Key Type: `Leader`

Tracks whether this instance currently holds the background-work lease.

- `Elect(ctx, locker Locker, opts LeaderOptions) *Leader` — starts campaigning in a goroutine and
  returns immediately (non-blocking). The campaign runs until `ctx` is cancelled, releasing the
  lease on cancellation so a surviving instance can take over within one retry tick instead of
  waiting out the TTL. A **nil `locker`** yields a `Leader` that is always in charge — the honest
  answer for a process with no coordination configured, which is by definition the only one.
- `LeaderOptions{Resource, Retry, OnChange}` — `Resource` defaults to `LeaderResource` ("leader");
  `Retry` defaults to `defaultLeaderRetry` (3s — deliberately shorter than the default 10s lease so
  a leader notices a lost lease before its replacement has run long, and a follower takes over
  promptly after a leader dies); `OnChange(isLeader bool)` fires (in its own goroutine, must not
  block) only on a leadership TRANSITION, for a log line or a metric.
- `(*Leader).IsLeader() bool` — the read every caller should use, asked EACH TIME they are about to
  do the work rather than cached, because leadership moves and the point of asking is to notice. A
  **nil `*Leader`** reports `true` (a caller never wired for coordination should behave as
  standalone — background work silently stopping is the worse failure mode than a duplicate).
- `campaign(ctx)` — on each retry tick, `attempt(ctx)`:
  - If already holding a lock, calls `held.Valid(ctx)`. Still valid: no-op (confirms leadership).
    Invalid (lease lost silently — a stall past the TTL, or the store dropping the key): stands
    down (`set(false)`) BEFORE trying to re-acquire, so the gap is a moment with no leader rather
    than a moment with two.
  - Not currently holding: calls `locker.TryLock(ctx, resource)`. `ErrNotAcquired` (somebody else
    is leading — the normal steady state) and any other error (the store is unreachable, and "I
    cannot tell" reads as "not in charge") both result in `set(false)`.
  - `resign()` on `ctx.Done()` (own goroutine's context, at shutdown): releases the held lease
    under a fresh 5-second timeout (the campaign's own context is already cancelled) so the next
    instance need not wait out the TTL.

## Wiring

`infra/apphost/run.go`'s `runApp` builds the one `*Leader` per process — `coordination.Elect(schedulerCtx, txLocker, coordination.LeaderOptions{OnChange: ...})`, logged on gain/loss — and hands it to every app as `Dependencies.Leader` (`types.go.md`). Apps gate scheduled singleton work on `deps.Leader.IsLeader()` inside the task body (see `apps/myseliasan/app/app.go.md`'s `leaderOnly`/`leaderTicker` helpers, `apps/myidsan/app/audit_retention.go.md`, `domain/notification/rollup.go`'s `RollupMaintainer.WithGate`).

## Notes

- `LeaderResource` ("leader") is exported so an operator reading Redis keys can recognise the
  lease; distinct from `infra/apphost`'s `migrationLockResource` ("bootstrap-migration"), which
  uses the queueing `Lock` for a different reason (see `migration_lock.go.md`).
- Checked per-tick / inside the task body, never once at task registration: a change of leadership
  can happen mid-run, and a loop that decided at startup would either never start working when
  promoted or never stop when demoted.
