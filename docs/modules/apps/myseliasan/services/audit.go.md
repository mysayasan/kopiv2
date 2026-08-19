# Module: apps/myseliasan/services/audit.go

## Purpose

myseliasan's slice of the audit trail. The trail itself — entity, record/list/purge,
retention — now lives in `domain/shared/audit` (`docs/modules/domain/shared/audit/service.go.md`):
it was never really control-plane-specific, myidsan had already written the same entity,
service and API independently, and the two had begun to drift (only myidsan truncated
hostile input, only myidsan had retention, only myidsan recorded the user agent). What stays
in this file is the part that is genuinely myseliasan's: the control-plane action vocabulary,
and the constructor that wires the shared service to myseliasan's own database.

## Constructor

`NewAuditService(db, logf)` — now `sharedaudit.NewServiceFromDb(db, logf)` under the hood.
Same signature as before; wired once in `app.go` and shared across every API that audits an
action.

## Responsibilities

- Type aliases onto `domain/shared/audit`: `AuditEntry = sharedaudit.Entry`,
  `AuditFilter = sharedaudit.Filter` (**new** — myseliasan's own `List` previously took three
  bare strings; it now inherits filtering by outcome, actor and a `CreatedAt` date range for
  free), `IAuditService = sharedaudit.IService`.
- `Outcome*` constants (`OutcomeSuccess`/`OutcomeDenied`/`OutcomeError`), re-exported from
  the shared package so call sites need no second import.
- Control-plane `Action*` constants, kept per-app rather than shared (the verbs are what THIS
  app does): `ActionNodeAdopt`, `ActionNodeRelease`, `ActionNodeBlock`, `ActionNodeForget`,
  `ActionNodeCommand`, `ActionRbacSetRole`, `ActionFleetKeyRotate`, `ActionBackupExport`,
  `ActionBackupRestore`.

## Notes

- Only `Record` and `List` are exposed — no update or delete — so the trail is tamper-evident; see `entities/audit_log.go.md`.
- `List(ctx, limit, offset, AuditFilter)` replaced the old `List(ctx, limit, offset, action, targetType, targetId string)` — every existing caller (`apis/audit.go`, `services/reports.go`) was updated to pass a `Filter` value. `limit` still defaults to 100, now capped at 1000 (was 500) to match the shared service.
- `List` treats "no rows matched" the same as an empty result rather than an error.
- `apis/audit.go` calls `List` for the superadmin-gated read API — now with outcome/actor/date-range filtering it did not have before; `apis/nodes.go`, `apis/rbac_admin.go`, `apis/node_access_api.go`, and `apis/node_proxy.go` call `Record` from their sensitive-action handlers.
- Age-based retention is now available (`sharedaudit.WithMetrics`/`PurgeOlderThan`, `domain/shared/audit/service.go.md`) but myseliasan does not yet wire a scheduler task for it the way `apps/myidsan/app/audit_retention.go` does — the capability exists in the shared service, adopting it here is unstarted work.
