# Module: infra/config/audit_retention.go

## Purpose

Config model and resolver for age-based retention of myidsan's append-only audit trail
(`apps/myidsan/entities/audit_log.go.md`) — the one deliberate exception to that trail having
no delete path, shaped so it cannot become a way to quietly erase an inconvenient event (see
`docs/MYIDSAN_PRODUCTIZATION_PLAN.md` Phase 4).

## Responsibilities

- `AuditRetentionConfigModel` (`audit` top-level config block, nested under `retention`) —
  `Enabled` (`bool`, default `false`: unbounded growth is a disk problem, silent deletion of
  security history is a compliance/forensics problem, and only one of those is recoverable),
  `MaxRetentionDays` (`int`), `FrequencyHours` (`int`), `ArchiveDir` (`string`). There is
  deliberately no API for any of this — it is config-file-only, so trimming the trail
  requires filesystem access to the server rather than a session on it, and even then an
  operator chooses only an age, never which rows.
- `(AuditRetentionConfigModel) EffectiveAuditRetention() (cfg AuditRetentionEffective, clamped bool)`
  — the only sanctioned read path (never read the struct fields directly):
  - `MaxRetentionDays` defaults to `365` when `<= 0`, and is raised to the 30-day floor
    (`minAuditRetentionDays`) when configured below it, with `clamped` reporting the raise so
    the caller can warn — a trail trimmed to a week answers almost no investigative question.
  - `FrequencyHours` defaults to `24` when `<= 0`.
  - `ArchiveDir` defaults to `audit-archive` when blank (trimmed).
- `AuditRetentionEffective` — the resolved form, every field guaranteed usable.
- `MinAuditRetentionDays() int` — exposes the 30-day floor for warning messages and tests.

## Notes

- Consumed by `apps/myidsan/app/audit_retention.go.md`'s `startAuditRetention`, which is a
  no-op unless `Enabled` is true, and by `apps/myidsan/services/audit_retention.go.md`'s
  `PurgeOlderThan`, which performs the archive-then-delete.
- Covered by `infra/config/audit_retention_test.go`: default resolution, the 30-day clamp
  (and its `clamped` flag), and archive-dir defaulting/trimming.
