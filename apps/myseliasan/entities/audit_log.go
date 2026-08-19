package entities

import sharedaudit "github.com/mysayasan/kopiv2/domain/shared/audit"

// AuditLog now lives in domain/shared/audit — see apps/myseliasan/services/audit.go for
// why it moved. The alias keeps every existing reference (reports, backup, the API) and
// the bootstrap entity registration compiling unchanged.
//
// The table is unaffected: the schema bootstrapper derives the table name from the STRUCT
// name, which is still AuditLog, so this keeps writing to the existing `audit_log` rows.
// The shared shape adds user_agent and an actor index, both applied additively by the
// auto-migrator.
type AuditLog = sharedaudit.AuditLog
