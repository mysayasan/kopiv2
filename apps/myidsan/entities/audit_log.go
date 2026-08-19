package entities

import sharedaudit "github.com/mysayasan/kopiv2/domain/shared/audit"

// AuditLog now lives in domain/shared/audit — see apps/myidsan/services/audit.go for why
// it moved. The alias keeps every existing reference and the bootstrap entity
// registration compiling unchanged, and the table is untouched: the schema bootstrapper
// derives the table name from the STRUCT name, which is still AuditLog.
type AuditLog = sharedaudit.AuditLog
