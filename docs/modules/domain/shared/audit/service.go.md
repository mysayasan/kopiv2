# Module: domain/shared/audit

## Purpose

The suite's append-only trail of security-relevant actions: who did what, to what, from where, and whether it worked.

It is shared because it had already been written twice. myidsan and myseliasan each grew their own entity, service and API — near-identical, and already diverging: only myidsan truncated hostile input, only myidsan had age-based retention, only myidsan recorded the user agent. mymatasan, the app that holds the actual video evidence, had none at all — deleting a recording recorded no actor and no reason. Three copies of an audit trail is three chances for the one that matters in an investigation to be the one nobody finished.

Files: `audit_log.go` (entity), `service.go` (record + read), `retention.go` (age-based archive-then-delete).

## The contract

**Entries are only ever INSERTED.** There is no update path and no targeted delete, so the trail cannot be edited from inside the product by the same superadmin whose actions it records. `IService` names exactly `Record`, `List` and `PurgeOlderThan` — a consumer holding the interface cannot reach anything else.

`PurgeOlderThan` is the single exception and is shaped so as not to weaken it: it takes an AGE rather than a selection of rows, it archives to a JSON-lines file and fsyncs it before deleting anything, it records its own run in the trail it trimmed (`audit.retention_purge`), and it must never be exposed over an API — each app drives it from a config setting that is off by default.

## Responsibilities

- `AuditLog` — the row. **The name stutters against the package on purpose**: the schema bootstrapper derives the table name from `strcase.ToSnake(typeOf.Name())`, so renaming it to `audit.Log` would silently start writing to a new `log` table and orphan every entry already recorded in myidsan and myseliasan.
- `Entry` — the caller-facing shape. The service fills `CreatedAt`, marshals `Metadata`, defaults `Outcome` to `success`, and truncates every free-text field at capture (a 10KB "username" on a failed login must not be able to bloat the table).
- `Filter` — narrows a listing by action, outcome, actor, target type/id and a `CreatedAt` range.
- `IService` / `NewService(repo, logf)` / `NewServiceFromDb(db, logf)`.
- `WithMetrics(svc, m, MetricNames)` — attaches a recorder. `MetricNames` is passed in because the suite's existing series are app-prefixed (`myidsan_audit_write_failures_total`), and renaming a shipped metric silently breaks whatever dashboard or alert is watching it.
- `PurgeOlderThan(ctx, maxRetentionDays, archiveDir)` → `PurgeResult{Archived, Deleted, ArchivePath, Cutoff}`.

## Notes

- **`Record` swallows its own write errors on purpose.** Auditing must never block or fail the action being audited — refusing a login because the audit table is full is worse than the gap. The consequence is that a trail which has stopped recording has NO other symptom, which is exactly why the write-failure counter exists; an app that configures `MetricNames` gets an alertable series, one that does not gets silence.
- **Retention archives before it deletes, and deletes nothing if the archive did not land.** Archive fully → `Sync` → rename into place → only then `Delete`. Any failure before the rename returns early having deleted nothing, so a full disk or a permissions mistake costs a retention run, never history. The purged set is stable while this runs precisely because the table is append-only: rows inserted after the cutoff is computed are stamped with the current time and can never fall into the window.
- **Action vocabularies stay per-app.** The verbs are what each app does, and one shared list of every app's actions would be a list nobody can read. See the `Action*` blocks in `apps/*/services/audit.go`.
- `ClientIp` must be resolved through `middlewares.ClientIP` with the app's trusted-proxy list at the call site. Trusting `X-Forwarded-For` unconditionally lets an untrusted caller choose the address recorded against their own action.
- **Never put a credential, token, TOTP secret, passphrase or password hash in `Metadata`.** The table is readable by every superadmin and is exported to CSV.
- Adopted by all three apps through type aliases (`AuditEntry = audit.Entry`, `IAuditService = audit.IService`, `entities.AuditLog = audit.AuditLog`), the same pattern `domain/shared/fleetnode` uses, so existing call sites are unchanged. myseliasan's own `List(limit, offset, action, targetType, targetId)` became `List(limit, offset, Filter)` and gained outcome/actor/date-range filtering; its report query now applies the date window in the QUERY rather than filtering the newest 500 rows afterwards, which previously let a busy period outside the window push in-window entries off the end.
- Schema impact on adoption: myidsan unchanged; myseliasan gains an additive `user_agent` column and an actor index; mymatasan gains the whole table. All handled by the auto-migrator — no migration.
- Covered by `service_test.go` (record defaults, best-effort write failure, filter + newest-first ordering), `metrics_test.go` (failure counted, success not counted, safe without a recorder, silent without configured names, purge counted) and `retention_test.go` + `retention_sqlite_test.go` (archive-then-delete ordering, nothing deleted when the archive cannot be written, archive kept when the delete fails, multi-batch paging, self-recording, and a run against real SQLite).
