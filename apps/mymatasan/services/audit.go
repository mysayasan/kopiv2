package services

import (
	sharedaudit "github.com/mysayasan/kopiv2/domain/shared/audit"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// The append-only audit trail, from domain/shared/audit.
//
// mymatasan shipped without one, which was the worst place in the suite for the gap:
// myidsan and myseliasan both recorded who did what, and mymatasan is the app that holds
// the actual video. Deleting a recording wrote no actor, no reason and no timestamp;
// neither did viewing or downloading one. RBAC already prevents an operator from deleting
// footage — an operator who was present at an incident must not be able to destroy the
// evidence of it — so the threat model was understood; there was simply no record when an
// administrator did it.
//
// "Who watched this footage, and when" is a mandatory line item in GDPR Article 30
// records and in essentially every government CCTV tender, and it cannot be reconstructed
// after the fact.

type (
	// AuditEntry is the caller-facing shape for recording an action.
	AuditEntry = sharedaudit.Entry
	// AuditFilter narrows a listing. Zero values mean "no filter on that field".
	AuditFilter = sharedaudit.Filter
	// IAuditService is the trail: record and read, never update or targeted-delete.
	IAuditService = sharedaudit.IService
	// PurgeResult reports what one age-based retention run did.
	PurgeResult = sharedaudit.PurgeResult
)

// Outcomes, re-exported so call sites need no second import.
const (
	OutcomeSuccess = sharedaudit.OutcomeSuccess
	OutcomeDenied  = sharedaudit.OutcomeDenied
	OutcomeError   = sharedaudit.OutcomeError
)

// Action names. Constants rather than inline strings so the set stays greppable and the
// UI filter can offer a closed list — an audit trail whose action names drift is one
// nobody can query.
//
// Convention: "<subject>.<verb>", lower_snake for multi-word verbs.
//
// The weighting is deliberate. Most audit trails record configuration changes and stop;
// this one leads with EVIDENCE HANDLING, because that is what an investigation asks about
// and what a tender asks to see. Viewing footage is as auditable as deleting it.
const (
	// Evidence handling. The reason this file exists.
	ActionRecordingView     = "recording.view"
	ActionRecordingDownload = "recording.download"
	ActionRecordingExport   = "recording.export"
	ActionRecordingDelete   = "recording.delete"
	ActionRecordingPurge    = "recording.purge"
	// ActionRecordingConfigChange covers retention: shortening it is a slower way of
	// deleting footage, so it belongs in the same story as the deletions above.
	ActionRecordingConfigChange = "recording.config_change"

	// Case files. The case's own trail is what the export bundle ships as a chain of
	// custody, so these are not administrative noise — they are the document.
	//
	// Note what is NOT here: exporting a case records ActionRecordingExport, not a
	// case-specific action, because "footage left the building" has to be answerable by
	// filtering on one action. A separate case.export would put half the evidence
	// handling outside the filter every auditor uses.
	ActionCaseCreate     = "case.create"
	ActionCaseUpdate     = "case.update"
	ActionCaseAssign     = "case.assign"
	ActionCaseClose      = "case.close"
	ActionCaseReopen     = "case.reopen"
	ActionCaseDelete     = "case.delete"
	ActionCaseItemAdd    = "case.item_add"
	ActionCaseItemUpdate = "case.item_update"
	ActionCaseItemRemove = "case.item_remove"

	// Video walls. What a control room is looking at is a fact worth being able to
	// reconstruct: "who took that camera off the wall, and when" is asked after an
	// incident, not before one.
	ActionWallChange = "wall.change"
	ActionWallDelete = "wall.delete"

	// Cameras. A credential change is recorded; the credential itself never is.
	ActionCameraCreate           = "camera.create"
	ActionCameraUpdate           = "camera.update"
	ActionCameraDelete           = "camera.delete"
	ActionCameraCredentialChange = "camera.credential_change"

	// Detection. Activating a taught skill changes what the system will and will not
	// notice, which is a security-relevant change even though it touches no footage.
	ActionVisionRuleChange   = "vision.rule_change"
	ActionTeachSkillActivate = "teach.skill_activate"

	// Accounts and authorization.
	ActionUserCreate     = "user.create"
	ActionUserUpdate     = "user.update"
	ActionUserDelete     = "user.delete"
	ActionUserRoleChange = "user.role_change"

	// App-level configuration.
	ActionSettingsChange = "settings.change"

	// Disaster recovery and destructive operations.
	ActionBackupExport  = "backup.export"
	ActionBackupRestore = "backup.restore"
	ActionSystemReset   = "system.reset"
	ActionSystemUpdate  = "system.update"
)

// AuditTargetType values, so a listing can be narrowed to one kind of thing.
const (
	TargetRecording = "recording"
	TargetCamera    = "camera"
	TargetUser      = "user"
	TargetSettings  = "settings"
	TargetSystem    = "system"
	TargetVision    = "vision"
	TargetCase      = "case"
	TargetWall      = "wall"
)

// NewAuditService builds the trail over mymatasan's database. logf receives write-failure
// diagnostics (may be nil).
func NewAuditService(db dbsql.IDbCrud, logf func(format string, args ...any)) IAuditService {
	return sharedaudit.NewServiceFromDb(db, logf)
}

// WithAuditMetrics attaches the recorder under mymatasan's series names, so a trail that
// has silently stopped recording becomes visible. Record() swallows its own write errors
// by design — auditing must never fail the action being audited — which means a broken
// trail otherwise has no symptom at all.
func WithAuditMetrics(svc IAuditService, m telemetry.Metrics) IAuditService {
	return sharedaudit.WithMetrics(svc, m, sharedaudit.MetricNames{
		WriteFailuresTotal:   MetricAuditWriteFailuresTotal,
		RetentionPurgedTotal: MetricAuditRetentionPurgedTotal,
	})
}
