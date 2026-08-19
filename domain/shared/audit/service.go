package audit

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// Outcomes. Kept as constants so the set stays closed and a UI filter can offer it.
const (
	OutcomeSuccess = "success"
	OutcomeDenied  = "denied"
	OutcomeError   = "error"
)

// Field caps. A hostile identifier — a 10KB "username" on a failed login, a pasted blob
// in a rename — must not be able to bloat the table. Applied at capture rather than at
// the column, because the trail has to keep accepting the event either way.
const (
	maxDetailLen     = 1024
	maxActionLen     = 128
	maxActorEmailLen = 320
	maxTargetTypeLen = 64
	maxTargetIdLen   = 256
	maxClientIpLen   = 64
	maxUserAgentLen  = 512
	maxMetadataLen   = maxDetailLen * 4
)

// Entry is the caller-facing shape for recording an event. The service fills in
// CreatedAt, marshals Metadata, truncates free text, and defaults Outcome to "success".
type Entry struct {
	Action     string
	ActorId    int64
	ActorEmail string
	ActorRole  int64
	TargetType string
	TargetId   string
	Outcome    string
	Detail     string
	Metadata   map[string]any
	ClientIp   string
	UserAgent  string
}

// Filter narrows a listing. Zero values mean "no filter on that field".
type Filter struct {
	Action     string
	Outcome    string
	ActorEmail string
	TargetType string
	TargetId   string
	// From/To bound CreatedAt inclusively (unix seconds).
	From int64
	To   int64
}

// MetricNames are the app-specific Prometheus series the service reports into. They are
// passed in rather than fixed here because the suite's existing series are app-prefixed
// (myidsan_audit_write_failures_total), and renaming a shipped metric silently breaks
// whatever dashboard or alert is watching it.
type MetricNames struct {
	// WriteFailuresTotal counts entries that could not be persisted.
	WriteFailuresTotal string
	// RetentionPurgedTotal counts rows removed by age-based retention.
	RetentionPurgedTotal string
}

// IService is the append-only trail. It exposes recording and reading, and deliberately
// no update and no targeted delete, so the trail cannot be edited from inside the product
// by the same superadmin whose actions it records.
//
// PurgeOlderThan is the single exception and is shaped so as not to weaken that: it takes
// an age rather than a selection of rows, it archives to disk before it deletes anything,
// and it records its own run in the trail. Trimming disk usage is possible; quietly
// removing a specific inconvenient event is not. Do not expose it over an API.
type IService interface {
	// Record persists one entry. It is best-effort by design: a write failure is logged
	// but never returned, so auditing can never block or fail the action being audited.
	// The alternative — refusing a login because the audit table is full — is worse.
	Record(ctx context.Context, e Entry)
	// List returns entries newest-first, narrowed by f.
	List(ctx context.Context, limit, offset uint64, f Filter) ([]*AuditLog, uint64, error)
	// PurgeOlderThan archives entries older than maxRetentionDays to a file under
	// archiveDir and then removes them from the table. It deletes nothing unless the
	// archive was written and flushed successfully. See retention.go.
	PurgeOlderThan(ctx context.Context, maxRetentionDays int, archiveDir string) (PurgeResult, error)
}

type service struct {
	repo dbsql.IGenericRepo[AuditLog]
	logf func(format string, args ...any)
	// metrics is optional. It exists for one counter, and that counter exists because
	// this service swallows its own write errors on purpose: auditing must never fail the
	// action being audited, which means a broken trail has no symptom at all.
	metrics telemetry.Metrics
	names   MetricNames
}

// NewService builds the trail over an existing repo. logf receives write-failure
// diagnostics so a silently-failing trail is still visible somewhere (may be nil).
func NewService(repo dbsql.IGenericRepo[AuditLog], logf func(format string, args ...any)) IService {
	return &service{repo: repo, logf: logf}
}

// NewServiceFromDb is the convenience form for apps that hold a dbsql.IDbCrud rather than
// a typed repo.
func NewServiceFromDb(db dbsql.IDbCrud, logf func(format string, args ...any)) IService {
	return NewService(dbsql.NewGenericRepo[AuditLog](db), logf)
}

// WithMetrics attaches a recorder and the app's series names so audit write failures and
// retention purges become observable. Optional and chainable.
func (s *service) WithMetrics(m telemetry.Metrics, names MetricNames) IService {
	s.metrics = m
	s.names = names
	return s
}

// WithMetrics attaches metrics to a service built by NewService/NewServiceFromDb. It is a
// free function because IService deliberately does not carry the setter — only the
// composition root configures telemetry, and every other holder of the interface should
// see recording and reading and nothing else.
func WithMetrics(svc IService, m telemetry.Metrics, names MetricNames) IService {
	if impl, ok := svc.(*service); ok {
		return impl.WithMetrics(m, names)
	}
	return svc
}

func (s *service) Record(ctx context.Context, e Entry) {
	outcome := strings.TrimSpace(e.Outcome)
	if outcome == "" {
		outcome = OutcomeSuccess
	}
	meta := ""
	if len(e.Metadata) > 0 {
		if b, err := json.Marshal(e.Metadata); err == nil {
			meta = truncate(string(b), maxMetadataLen)
		}
	}
	row := AuditLog{
		Action:     truncate(e.Action, maxActionLen),
		ActorId:    e.ActorId,
		ActorEmail: truncate(e.ActorEmail, maxActorEmailLen),
		ActorRole:  e.ActorRole,
		TargetType: truncate(e.TargetType, maxTargetTypeLen),
		TargetId:   truncate(e.TargetId, maxTargetIdLen),
		Outcome:    outcome,
		Detail:     truncate(e.Detail, maxDetailLen),
		Metadata:   meta,
		ClientIp:   truncate(e.ClientIp, maxClientIpLen),
		UserAgent:  truncate(e.UserAgent, maxUserAgentLen),
		CreatedAt:  time.Now().Unix(),
	}
	if _, err := s.repo.Create(ctx, "", row); err != nil {
		// Counted before logging: the log line is what an operator reads if they already
		// suspect a problem, the counter is what tells them there IS one.
		if s.metrics != nil && s.names.WriteFailuresTotal != "" {
			s.metrics.Inc(s.names.WriteFailuresTotal, nil)
		}
		if s.logf != nil {
			s.logf("audit write failed for %q on %s/%s: %v", e.Action, e.TargetType, e.TargetId, err)
		}
	}
}

func (s *service) List(ctx context.Context, limit, offset uint64, f Filter) ([]*AuditLog, uint64, error) {
	if limit == 0 || limit > 1000 {
		limit = 100
	}
	var filters []sqldataenums.Filter
	add := func(field, value string) {
		if v := strings.TrimSpace(value); v != "" {
			filters = append(filters, sqldataenums.Filter{FieldName: field, Compare: sqldataenums.Equal, Value: v})
		}
	}
	add("Action", f.Action)
	add("Outcome", f.Outcome)
	add("ActorEmail", f.ActorEmail)
	add("TargetType", f.TargetType)
	add("TargetId", f.TargetId)
	if f.From > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CreatedAt", Compare: sqldataenums.GreaterThanOrEqualTo, Value: f.From})
	}
	if f.To > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CreatedAt", Compare: sqldataenums.LessThanOrEqualTo, Value: f.To})
	}

	// Newest first by primary key: CreatedAt has second resolution, so several events in
	// the same second would otherwise come back in an arbitrary order.
	sorters := []sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.DESC}}
	rows, total, err := s.repo.Get(ctx, "", limit, offset, filters, sorters)
	if err != nil {
		if IsNotFound(err) {
			return []*AuditLog{}, 0, nil
		}
		return nil, 0, err
	}
	return rows, total, nil
}

// IsNotFound reports the generic repo's "no rows" sentinel, which it returns as a plain
// error string rather than a typed value.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "no result found")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
