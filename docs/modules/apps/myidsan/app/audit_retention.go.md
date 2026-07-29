# Module: apps/myidsan/app/audit_retention.go

## Purpose

Wires the audit-log retention purge (`services/audit_retention.go.md`) into the shared
`apphost.Dependencies.Scheduler` as a periodic background task, gated entirely on
`config.audit.retention.enabled`.

## Responsibilities

- `startAuditRetention(deps apphost.Dependencies, auditService services.IAuditService)`,
  called from `RegisterAppRoutes` (`app/app.go.md`) right after `services.NewAuditService` is
  constructed:
  - No-ops immediately when `deps.Config` or `deps.Scheduler` is nil (test/minimal wiring), or
    when `deps.Config.Audit.EffectiveAuditRetention()` resolves `Enabled` false — the shipped
    default, so an install that never touches the config block never runs a purge.
  - Logs a `WARNING` when `EffectiveAuditRetention` reports `clamped` (a configured
    `maxRetentionDays` below the 30-day floor was raised), so an operator who set a too-short
    window is told rather than silently getting a longer one.
  - Resolves `archiveDir` via `apphost.ResolveWritablePath(deps.DataDir, cfg.ArchiveDir)` — a
    relative `archiveDir` is data-dir-relative.
  - Registers `deps.Scheduler.StartPeriodic("myidsan-audit-retention",
    cfg.FrequencyHours*time.Hour, ...)`. The scheduler fires the first tick after one
    interval, not at boot, so a restart loop cannot turn into a purge loop.
  - The task callback calls `auditService.PurgeOlderThan(taskCtx, cfg.MaxRetentionDays,
    archiveDir)`; a returned error is propagated to the scheduler (logged, retried next tick)
    rather than escalated — a retention failure must never take the identity server down.
    A run that actually deleted rows (`res.Deleted > 0`) logs a one-line summary
    (archived/deleted counts, cutoff, archive path).

- `startSessionGauge(deps apphost.Dependencies, sessions services.ISessionService)`, called
  from `RegisterAppRoutes` right after `services.NewSessionService` is constructed:
  - No-ops when `deps.Scheduler`, `deps.Metrics`, or `sessions` is nil.
  - Registers `deps.Scheduler.StartPeriodic("myidsan-session-gauge", sessionGaugeInterval,
    ...)` (`sessionGaugeInterval = time.Minute`), whose task calls
    `services.PublishActiveSessions(taskCtx, sessions, deps.Metrics)` — see
    `services/session.go.md`, `services/metrics.go.md`.
  - **Polled, not incremented at call sites**: a session also ends without anyone calling
    `Revoke` (a cache entry simply expires on its own), so a hand-maintained counter would
    drift from the truth and never recover, while a periodic read of the index is at worst
    one interval stale. The one-minute period is a deliberate compromise — often enough
    that a revocation shows up on a dashboard while an operator is still looking at it,
    rare enough that it costs one trivial `COUNT` per minute rather than being a load
    source of its own.

## Notes

- Config resolution lives in `infra/config/audit_retention.go.md`
  (`AuditRetentionConfigModel.EffectiveAuditRetention`); this file only consumes the
  resolved `AuditRetentionEffective` and the `clamped` flag.
- See `docs/MYIDSAN_PRODUCTIZATION_PLAN.md` Phase 4 and `apps/myidsan/README.md`'s Audit log
  bullet for the operator-facing description.
- Despite the filename, this file also hosts `startSessionGauge` — the session-gauge
  scheduler task, unrelated to audit retention — because it is small, is wired from the
  same block of `RegisterAppRoutes`, and shares the "periodic `deps.Scheduler` task,
  no-op unless its dependencies are present" shape with `startAuditRetention`.
