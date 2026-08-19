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
	ActionNodeAdopt     = "node.adopt"
	ActionNodeRelease   = "node.release"
	ActionNodeBlock     = "node.block"
	ActionNodeForget    = "node.forget"
	ActionNodeCommand   = "node.command"
	ActionRbacSetRole   = "rbac.set_role"
	ActionFleetKeyRotate = "fleet.key_rotate"
	ActionBackupExport  = "backup.export"
	ActionBackupRestore = "backup.restore"
)

// NewAuditService builds the trail over the control plane's database. logf receives
// write-failure diagnostics (may be nil).
func NewAuditService(db dbsql.IDbCrud, logf func(format string, args ...any)) IAuditService {
	return sharedaudit.NewServiceFromDb(db, logf)
}
