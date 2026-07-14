# Module: infra/safego/safego.go

## Purpose

Runs background goroutines that cannot take the whole process down. Before this package
there was exactly one `recover()` in the entire repo (`infra/notification/hub.go`); the
worst case was the vision monitor, which runs one goroutine per camera pushing detector
payloads from the Python worker through the alert path — a single malformed detection
(a nil box, a bad label) panicked and killed the whole NVR, recording and API included.

## Responsibilities

- `Go(name string, fn func())` — runs `fn` in a goroutine, recovering any panic so it
  cannot take the process down. The goroutine is **not** restarted; use it for one-shot
  background work where a retry would be wrong (a warmup pass, a probe, a download).
- `Supervise(ctx context.Context, name string, fn func(context.Context))` — runs `fn` in
  a goroutine and, if it panics, restarts it with backoff until `ctx` is cancelled. Use it
  for long-lived loops (monitors, samplers, ticker-driven purges) whose death would
  silently disable a subsystem. A **clean return** from `fn` is treated as a deliberate
  stop (a loop exiting on `ctx.Done()`) and is not restarted — only a panic triggers a
  restart.
- `SetLogger(fn LogFunc)` — routes recovered panics into the application logger. Until
  called (or after `nil` is passed), panics go to the standard `log` package, so a panic
  is never silently lost even before the app wires its own logger.
- `PanicObserver func(component string)` / `SetPanicObserver(fn PanicObserver)` — routes every
  recovered panic to a metrics recorder as well as the logger. `runGuarded` (backing `Supervise`)
  and `recoverPanic` (backing `Go`) both call `notifyPanic(name)` right after `logf`, so a panic
  is counted whether the task is restarted or one-shot. This closes the gap "why restart, not
  just recover" below leaves open at the *observability* layer: a task can be caught and
  restarted and still leave nothing else a human would notice — the process stays up, the API
  answers, and the subsystem it drove has silently stopped. `infra/apphost/run.go` wires this
  once per app to a `<app>_task_panics_total{task}` counter (`run.go.md`). `nil` is the default
  (and what `SetPanicObserver(nil)` restores) — a panic is still logged with no observer wired,
  only not counted. Guarded by the same `RWMutex` as `logger`.

## Backoff

- `initialBackoff` = 1s, doubling on each consecutive panic, capped at `maxBackoff` = 30s
  — enough to avoid spinning a task that panics every time, while still retrying often
  enough to recover from a transient fault.
- `healthyRun` = 2 minutes: if a supervised task ran that long before panicking, its
  backoff resets to `initialBackoff` on the next restart. Without this a task that panics
  once an hour would creep up to the 30s cap and stay there.
- Backoff waits respect `ctx`, so cancelling it stops the supervisor immediately even
  mid-backoff — shutdown is never delayed by a task that is mid-restart.

## Why restart, not just recover

Recovering a panic is not enough for `Supervise`'s callers: nothing else notices the
goroutine is gone. The vision monitor is the concrete example — `VisionMonitor.reconcileSamplers`
only starts a `cameraSampler` when its `samplers` map has no entry for that camera, so a
sampler goroutine that died (recovered, not restarted) would leave a live map entry behind
and that camera would silently stop being watched until the next process restart.

## Notes

- Both `Go` and `Supervise` log via `runtime/debug.Stack()` alongside the recovered value,
  so a recovered panic still carries a full stack trace into the log.
- `logger` is guarded by an `RWMutex` so `SetLogger` can be called concurrently with
  in-flight recoveries.
- Tests: `safego_test.go` — a panicking one-shot is recovered and logged
  (`TestGo_RecoversPanic`); a panicking supervised task restarts
  (`TestSupervise_RestartsAfterPanic`); a clean return is not restarted
  (`TestSupervise_DoesNotRestartOnCleanReturn`); cancelling `ctx` stops a supervisor even
  while it is backing off (`TestSupervise_StopsOnContextCancel`). `observer_test.go` —
  every recovered panic in a crash-looping supervised task is counted, once per panic, with the
  task name (`TestPanicObserver_CountsEveryRecoveredPanic`); a one-shot `Go` panic is counted too,
  even though it is never restarted (`TestPanicObserver_CountsAOneShotPanic`).
