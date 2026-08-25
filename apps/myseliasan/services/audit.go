package services

import (
	sharedaudit "github.com/mysayasan/kopiv2/domain/shared/audit"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// The audit trail now lives in domain/shared/audit, because it was never
// control-plane-specific. myidsan had written the same entity, service and API
// independently, and the two had already begun to drift — only myidsan truncated hostile
// input, only myidsan had retention, only myidsan recorded the user agent. mymatasan, the
// app holding the actual video, had no trail at all. Three copies of an audit log is three
// chances for the one that matters in an investigation to be the one nobody finished.
//
// These aliases keep myseliasan's call sites unchanged. The shared entity keeps the
// struct name AuditLog, so it still maps to the existing `audit_log` table; the only
// schema change is the additive user_agent column and the actor index, both of which the
// auto-migrator applies on first boot.

type (
	// AuditEntry is the caller-facing shape for recording a sensitive action.
	AuditEntry = sharedaudit.Entry
	// AuditFilter narrows a listing. New here — myseliasan's own List took three bare
	// strings, and inherits filtering by outcome, actor and date range from the shared one.
	AuditFilter = sharedaudit.Filter
	// IAuditService is the append-only trail: record and read, never update or delete.
	IAuditService = sharedaudit.IService
)

// Outcome values, re-exported so call sites need no second import.
const (
	OutcomeSuccess = sharedaudit.OutcomeSuccess
	OutcomeDenied  = sharedaudit.OutcomeDenied
	OutcomeError   = sharedaudit.OutcomeError
)

// Control-plane action names. Kept per-app rather than shared: the verbs are what THIS
// app does, and a shared list of every app's actions would be a list nobody can read.
//
// Convention: "<subject>.<verb>", lower_snake for multi-word verbs.
const (
	ActionNodeAdopt      = "node.adopt"
	ActionNodeRelease    = "node.release"
	ActionNodeBlock      = "node.block"
	ActionNodeForget     = "node.forget"
	ActionNodeCommand    = "node.command"
	ActionRbacSetRole    = "rbac.set_role"
	ActionFleetKeyRotate = "fleet.key_rotate"
	ActionBackupExport   = "backup.export"
	ActionBackupRestore  = "backup.restore"
	// ActionPolicyEnforce records a fleet policy writing a setting back to a node. It is
	// the only settings change on a node with no operator behind it — it happens on a
	// timer, possibly long after the policy was written — so without this entry the
	// node's own trail shows an admin action with no admin anywhere near it.
	ActionPolicyEnforce = "policy.enforce"
	// ActionPolicySave / ActionPolicyDelete record edits to the estate's configuration
	// standard itself. Worth recording separately from the enforcement it causes: the
	// enforcement says a node changed, this says who decided it should.
	ActionPolicySave   = "policy.save"
	ActionPolicyDelete = "policy.delete"
	// N+1 failover (W3-7). WHICH APPLIANCE IS RESPONSIBLE for a building's cameras is the
	// question a gap in the footage turns into, months later, in front of somebody who
	// was not there. Every step is recorded, including the automatic takeover — which is
	// the one with no operator behind it and therefore the one that would otherwise leave
	// no trace anywhere but a log line.
	ActionFailoverPlanSave   = "failover.plan_save"
	ActionFailoverPlanDelete = "failover.plan_delete"
	ActionFailoverStage      = "failover.stage"
	ActionFailoverDrill      = "failover.drill"
	ActionFailoverActivate   = "failover.activate"
	ActionFailoverRelease    = "failover.release"
	// Mobile push (W3-9). Registering a device means this appliance starts making outbound
	// requests to a browser vendor on somebody's behalf, and removing one silences an alert
	// path somebody was relying on — both are worth a line. The ENDPOINT is never recorded:
	// it is a third-party identifier for a personal device, and a trail is read later by
	// people who never needed to know which phone anybody carries.
	// Fleet video walls (W3-3d). A wall is a shared arrangement several people watch, so
	// changing one changes what everybody sees.
	ActionFleetWallSave   = "fleet_wall.save"
	ActionFleetWallDelete = "fleet_wall.delete"

	ActionPushSubscribe   = "push.subscribe"
	ActionPushUnsubscribe = "push.unsubscribe"
	ActionPushTest        = "push.test"
)

// NewAuditService builds the trail over the control plane's database. logf receives
// write-failure diagnostics (may be nil).
func NewAuditService(db dbsql.IDbCrud, logf func(format string, args ...any)) IAuditService {
	return sharedaudit.NewServiceFromDb(db, logf)
}
