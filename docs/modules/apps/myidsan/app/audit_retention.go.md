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

## Notes

- Config resolution lives in `infra/config/audit_retention.go.md`
  (`AuditRetentionConfigModel.EffectiveAuditRetention`); this file only consumes the
  resolved `AuditRetentionEffective` and the `clamped` flag.
- See `docs/MYIDSAN_PRODUCTIZATION_PLAN.md` Phase 4 and `apps/myidsan/README.md`'s Audit log
  bullet for the operator-facing description.
