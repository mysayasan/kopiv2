# Module: apps/myidsan/services/audit.go

## Purpose

`IAuditService` — the append-only security trail: record, list, and one narrowly-shaped
retention purge. Deliberately no update and no targeted delete, so the trail cannot be
edited from inside the product by the same superadmin whose actions it records (see
`entities/audit_log.go.md`).

## Responsibilities

- `Action*` constants (`ActionLoginSuccess`, `ActionLoginFailure`, `ActionLoginLockout`,
  `ActionLogout`, `ActionMfaChallenge`, `ActionPasswordReset`, `ActionMfaEnroll`,
  `ActionMfaDisable`, `ActionMfaAdminReset`, `ActionMfaRecovery`, `ActionMfaRegenerate`,
  `ActionUserCreate`, `ActionUserUpdate`, `ActionUserDelete`, `ActionUserRoleChange`,
  `ActionPasswordChange`, `ActionRoleCreate/Update/Delete`, `ActionPermissionSet`,
  `ActionAppCreate/Update/Delete`, `ActionAppSecretRotate`, `ActionAppRedirectSet`,
  `ActionDirectoryUpdate`, `ActionGroupMapChange`, `ActionStepUpSuccess/Failure`,
  `ActionSessionRevoke`/`ActionSessionRevokeAll`, `ActionBackupExport`/`ActionBackupRestore`)
  are kept as named constants, convention `<subject>.<verb>`, rather than inline strings, so
  the set stays greppable and a UI filter can offer a closed list — an audit trail whose
  action names drift is one nobody can query. `Outcome*` constants (`success`/`denied`/
  `error`) and `Method*` sign-in-method constants (`local`/`ldap`/`kerberos`/`oidc`/
  `social`/`recovery_code`) live alongside them.
- `AuditEntry` is the caller-facing shape for recording an event; the service fills in
  `CreatedAt`, marshals `Metadata`, and defaults `Outcome` to `success`.
- `AuditFilter` narrows a listing (`Action`, `Outcome`, `ActorEmail`, `TargetType`,
  `TargetId`, and an inclusive `From`/`To` unix-second `CreatedAt` range). Zero values mean
  no filter on that field.
- `Record(ctx, e)` persists one entry and is **best-effort by design**: a write failure is
  counted (`MetricAuditWriteFailuresTotal`, when a metrics recorder is attached) and logged
  (via the injected `logf`) but never returned, so auditing can never block or fail the
  action being audited — refusing a login because the audit table is full would be worse
  than a gap in the trail. The counter exists because that swallowed error is otherwise the
  ONLY trace: every other signal stays green while the trail quietly stops recording (see
  `services/metrics.go.md`). Every free-text field is length-truncated before insert
  (`truncate`, e.g. 1024 bytes for `Detail`, 320 for `ActorEmail`) so a hostile identifier
  (a huge "username" on a failed login) cannot bloat the table.
- `List(ctx, limit, offset, f)` applies `AuditFilter` as `Equal`/range filters and sorts
  newest-first by `Id` (not `CreatedAt` alone — that field has only second resolution, so
  several events in the same second would otherwise come back in an arbitrary order).
  `limit` defaults/caps to 100/1000.
- `PurgeOlderThan(ctx, maxRetentionDays, archiveDir)` — the one exception to "no delete",
  implemented in `services/audit_retention.go.md`. It cannot be reached over the API: it
  runs only from `startAuditRetention` (`app/audit_retention.go.md`) when
  `config.audit.retention.enabled` is on. It takes an age, never a selection of rows;
  archives every row older than the cutoff to a JSON-lines file under `archiveDir` and
  fsyncs/renames it into place **before** deleting anything from the table, so a run that
  cannot finish its archive deletes nothing; and it records its own run in the trail
  (`ActionAuditPurge = "audit.retention_purge"`) naming the cutoff, row count, and archive
  file, so a reader who finds history starting abruptly can tell a deliberate trim from an
  empty past.

## Notes

- `NewAuditService(repo, logf)` takes a plain `dbsql.IGenericRepo[entities.AuditLog]` and a
  `func(format string, args ...any)` diagnostics sink (may be `nil`), so a silently-failing
  trail is still visible somewhere.
- `WithMetrics(m telemetry.Metrics) IAuditService` attaches a recorder after construction
  and returns the same service (chainable), so the one existing construction site in
  `app/app.go.md` only needed a follow-up call, not a new constructor parameter. `metrics`
  is optional and nil-safe — `Record`/`PurgeOlderThan` work identically without it, just
  without the counters.
- Constructed once in `app/app.go.md` and passed by value into every handler that performs
  a sensitive action, rather than reached through a global.
