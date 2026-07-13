package services

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// AuditEntry is the caller-facing shape for recording a sensitive action. The
// service fills in CreatedAt and marshals Metadata; Outcome defaults to "success".
type AuditEntry struct {
	Action     string
	ActorId    int64
	ActorEmail string
	ActorRole  int64
	TargetType string
	TargetId   string
	Outcome    string // "success" (default) | "denied" | "error"
	Detail     string
	Metadata   map[string]any
	ClientIp   string
}

// IAuditService is the append-only audit trail for sensitive control-plane actions.
// It exposes only recording and reading — never update or delete — so the trail is
// tamper-evident.
type IAuditService interface {
	// Record persists one audit entry. It is best-effort: a failure to write is logged
	// but never propagated, so auditing can never block or fail the audited action.
	Record(ctx context.Context, e AuditEntry)
	// List returns audit entries newest-first, optionally narrowed by action /
	// target type / target id (empty = no filter on that field).
	List(ctx context.Context, limit, offset uint64, action, targetType, targetId string) ([]*entities.AuditLog, uint64, error)
}

type auditService struct {
	repo dbsql.IGenericRepo[entities.AuditLog]
	logf func(format string, args ...any)
}

// NewAuditService builds the audit service over its own table. logf receives
// write-failure diagnostics (may be nil).
func NewAuditService(db dbsql.IDbCrud, logf func(format string, args ...any)) IAuditService {
	return &auditService{
		repo: dbsql.NewGenericRepo[entities.AuditLog](db),
		logf: logf,
	}
}

func (s *auditService) Record(ctx context.Context, e AuditEntry) {
	outcome := e.Outcome
	if outcome == "" {
		outcome = "success"
	}
	meta := ""
	if len(e.Metadata) > 0 {
		if b, err := json.Marshal(e.Metadata); err == nil {
			meta = string(b)
		}
	}
	row := entities.AuditLog{
		Action:     e.Action,
		ActorId:    e.ActorId,
		ActorEmail: e.ActorEmail,
		ActorRole:  e.ActorRole,
		TargetType: e.TargetType,
		TargetId:   e.TargetId,
		Outcome:    outcome,
		Detail:     e.Detail,
		Metadata:   meta,
		ClientIp:   e.ClientIp,
		CreatedAt:  time.Now().Unix(),
	}
	if _, err := s.repo.Create(ctx, "", row); err != nil && s.logf != nil {
		s.logf("audit write failed for %q on %s/%s: %v", e.Action, e.TargetType, e.TargetId, err)
	}
}

func (s *auditService) List(ctx context.Context, limit, offset uint64, action, targetType, targetId string) ([]*entities.AuditLog, uint64, error) {
	if limit == 0 || limit > 500 {
		limit = 100
	}
	var filters []sqldataenums.Filter
	if action != "" {
		filters = append(filters, sqldataenums.Filter{FieldName: "Action", Compare: sqldataenums.Equal, Value: action})
	}
	if targetType != "" {
		filters = append(filters, sqldataenums.Filter{FieldName: "TargetType", Compare: sqldataenums.Equal, Value: targetType})
	}
	if targetId != "" {
		filters = append(filters, sqldataenums.Filter{FieldName: "TargetId", Compare: sqldataenums.Equal, Value: targetId})
	}
	sorters := []sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.DESC}}
	rows, total, err := s.repo.Get(ctx, "", limit, offset, filters, sorters)
	if err != nil {
		if isNoResultFoundErr(err) {
			return []*entities.AuditLog{}, 0, nil
		}
		return nil, 0, err
	}
	return rows, total, nil
}
