# Module: infra/apphost/migration_lock.go

## Purpose

Wraps schema bootstrap (`bootstrap.Ensure`) in a deployment-wide lock so several instances
starting together (the common case — an orchestrator starts replicas at once) take turns
running `CREATE TABLE`/`ALTER TABLE`/seed `INSERT`s against one database instead of racing each
other. Schema bootstrap is the one startup step several instances can genuinely corrupt for each
other; everything else at startup either settles on its own or is itself guarded by this lock's
caller ordering.

## Responsibilities

- `withMigrationLock(ctx, appName string, appConfig *config.AppConfigModel, fn func() error) error`
  — called from `run.go`'s `runApp` around the `bootstrap.Ensure` call:
  - Resolves the effective lock provider the same way the process-wide transaction locker does
    (`transaction.lockProvider`, falling back to `cache.provider` when unset). When it is not a
    distributed provider (`sharedServices.IsDistributedLockProvider`), calls `fn()` directly with
    no locking at all — a lock nobody else can see would be pure overhead, and a standalone
    install has nothing to race with.
  - Otherwise builds a **short-lived locker of its own** (`buildTransactionLocker`, `nil`
    telemetry recorder) rather than reusing the process-wide one — the process-wide locker does
    not exist yet at this point in `runApp` (it needs the telemetry recorder, which needs the
    router), and boot-time coordination only needs to live for the seconds bootstrap takes.
    Closed via `defer locker.Close()`.
  - Acquires `migrationLockResource` ("bootstrap-migration") via the **queueing** `Locker.Lock`,
    not `TryLock` — a follower here must WAIT for the leader's schema work and then find nothing
    left to do, not skip it. Bounded by `migrationLockWait` (10 minutes — deliberately far longer
    than the ordinary transaction-lock default, since altering a large table is slow and giving up
    early would put concurrent DDL back on the table). `migrationLockWait` is passed to
    `buildTransactionLocker` as the locker's **own** `WaitTimeout`, not merely as a context
    deadline: `Lock` bounds its wait by the locker's configured `WaitTimeout` (30s by default), so
    a context deadline alone was decoration — a migration that ran longer than 30 seconds, which
    is exactly the case worth serializing, made every other instance give up and proceed WITHOUT
    the lock, straight into the concurrent DDL this exists to prevent.
  - A failure to **acquire** the lock (store unreachable, timeout) is logged and `fn()` runs
    anyway — refusing to boot because coordination was briefly unreachable would turn an
    availability feature into an outage, and single-instance installs (which never even reach this
    branch) are the overwhelming majority of deployments. Only a genuine unrecoverable failure
    inside `fn()` itself (i.e. `bootstrap.Ensure` erroring) propagates and aborts startup.
  - On success, logs that the lock is held, defers a release under its own 10-second timeout
    (logged, not fatal, on failure), and calls `fn()`.

## Notes

- Uses the same `coordination.Locker`/`Lock` primitives as the leader lease
  (`infra/coordination/leader.go.md`) and the process-wide transaction lock
  (`infra/coordination/locker.go.md`), but deliberately the **queueing** `Lock` rather than
  `TryLock` — see `Locker.TryLock`'s doc comment for why a loser here must wait rather than skip.
- `migrationLockResource` is a fixed, low-cardinality resource name distinct from
  `coordination.LeaderResource` ("leader"), so an operator inspecting Redis keys can tell the two
  concerns apart.
- Standalone behavior is unchanged: with the per-process (memory) lock provider — the default —
  `withMigrationLock` is a direct call-through to `fn()` with no lock built at all.
