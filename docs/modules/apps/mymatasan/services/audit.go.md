# Module: apps/mymatasan/services/audit.go

## Purpose

mymatasan's slice of the shared append-only audit trail (`domain/shared/audit`, `docs/modules/domain/shared/audit/service.go.md`): the action vocabulary and target-type constants this app records against, plus the two constructors that wire the shared service to mymatasan's own database and metric names.

mymatasan shipped without an audit trail at all — the worst place in the suite for the gap, since it is the app holding the actual video evidence. Deleting a recording recorded no actor and no reason; neither did viewing or downloading one. `apps/mymatasan/apis/audit.go` (`apis/audit.go.md`) is the read/record surface built on top of what this file declares.

## Responsibilities

- Type aliases onto `domain/shared/audit`: `AuditEntry`, `AuditFilter`, `IAuditService`, `PurgeResult`, and the `Outcome*` constants — the same pattern myidsan and myseliasan use, so call sites need no second import.
- Action constants, `"<subject>.<verb>"`, weighted toward evidence handling over configuration because that is what an investigation and a tender both ask about:
  - Evidence handling: `ActionRecordingView`, `ActionRecordingDownload`, `ActionRecordingExport`, `ActionRecordingDelete`, `ActionRecordingPurge`, `ActionRecordingConfigChange` (retention changes — shortening retention is a slower way of deleting footage).
  - Cameras: `ActionCameraCreate`, `ActionCameraUpdate`, `ActionCameraDelete`, `ActionCameraCredentialChange` (the username is recorded, the password never is).
  - Detection: `ActionVisionRuleChange`, `ActionTeachSkillActivate` — activating a taught skill changes what the system will and will not notice, a security-relevant change even though it touches no footage.
  - Accounts: `ActionUserCreate`, `ActionUserUpdate`, `ActionUserDelete`, `ActionUserRoleChange`.
  - App configuration: `ActionSettingsChange` — used generically, with the target id naming which settings block changed (e.g. `"tamper"`, `"continuity"`).
  - Destructive/DR: `ActionBackupExport`, `ActionBackupRestore`, `ActionSystemReset`, `ActionSystemUpdate`.
- Target-type constants: `TargetRecording`, `TargetCamera`, `TargetUser`, `TargetSettings`, `TargetSystem`, `TargetVision`.
- `NewAuditService(db dbsql.IDbCrud, logf) IAuditService` — builds the trail over mymatasan's database via `sharedaudit.NewServiceFromDb`.
- `WithAuditMetrics(svc, m telemetry.Metrics) IAuditService` — attaches the recorder under mymatasan's own series names (`MetricAuditWriteFailuresTotal` / `MetricAuditRetentionPurgedTotal`, `services/metrics.go.md`), so a trail that has silently stopped recording becomes observable. `Record()` swallows its own write errors by design (auditing must never fail the action being audited), which means this counter is the only symptom such a failure produces.

## Notes

- Not every constant declared here is used yet by a handler (e.g. `ActionVisionRuleChange`, `ActionTeachSkillActivate`, `ActionCameraCreate/Update/Delete`, `ActionSystemReset`, `ActionSystemUpdate`, `ActionBackupExport/Restore`) — they exist so the vocabulary is complete and reviewable in one place even as call sites are added incrementally. See `apis/audit.go.md`'s "What is recorded" table for what is wired today.
- Action vocabularies stay per-app by design (see `domain/shared/audit/service.go.md`): the verbs are what each app does, and one shared list of every app's actions would be a list nobody can read.
- Wired from `app.go`'s `RegisterAppRoutes`, built before the `wiring` struct so the handlers constructed from that struct can already record into it — see `apps/mymatasan/app/app.go.md`.
