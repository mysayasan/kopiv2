package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/myidsan/entities"
	sharedentities "github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/cache"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// countingMetrics records Inc/Add/Set calls by metric name.
type countingMetrics struct {
	telemetry.Metrics
	mu     sync.Mutex
	counts map[string]float64
	gauges map[string]float64
}

func newCountingMetrics() *countingMetrics {
	return &countingMetrics{counts: map[string]float64{}, gauges: map[string]float64{}}
}

func (m *countingMetrics) Describe(string, string) {}

func (m *countingMetrics) Inc(name string, _ telemetry.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[name]++
}

func (m *countingMetrics) Add(name string, _ telemetry.Labels, delta float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[name] += delta
}

func (m *countingMetrics) Set(name string, _ telemetry.Labels, v float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gauges[name] = v
}

func (m *countingMetrics) get(name string) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counts[name]
}

func (m *countingMetrics) gauge(name string) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gauges[name]
}

// failingAuditRepo rejects every write.
type failingAuditRepo struct {
	dbsql.IGenericRepo[entities.AuditLog]
}

func (failingAuditRepo) Create(context.Context, string, entities.AuditLog) (uint64, error) {
	return 0, errors.New("disk full")
}

func (failingAuditRepo) Get(context.Context, string, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter) ([]*entities.AuditLog, uint64, error) {
	return nil, 0, nil
}

// This counter is the reason the metric exists. Record() swallows its write error on
// purpose — auditing must never fail the action being audited — which means a trail that
// has stopped recording has NO other symptom. Every other signal stays green while the
// security history quietly develops a hole.
func TestAuditWriteFailureIsCounted(t *testing.T) {
	m := newCountingMetrics()
	svc := (&auditService{repo: failingAuditRepo{}}).WithMetrics(m)

	svc.Record(context.Background(), AuditEntry{Action: ActionLoginSuccess, ActorEmail: "a@example.test"})
	svc.Record(context.Background(), AuditEntry{Action: ActionLoginFailure, ActorEmail: "b@example.test"})

	if got := m.get(MetricAuditWriteFailuresTotal); got != 2 {
		t.Fatalf("%s = %v want 2 — a trail that stopped recording would look identical to a healthy one", MetricAuditWriteFailuresTotal, got)
	}
}

// The counter must stay at zero on the happy path, or it is useless as an alert.
func TestAuditWriteSuccessIsNotCounted(t *testing.T) {
	m := newCountingMetrics()
	repo := &fakeAuditRepo{}
	svc := (&auditService{repo: repo}).WithMetrics(m)

	svc.Record(context.Background(), AuditEntry{Action: ActionLoginSuccess, ActorEmail: "a@example.test"})

	if got := m.get(MetricAuditWriteFailuresTotal); got != 0 {
		t.Fatalf("%s = %v want 0 on a successful write", MetricAuditWriteFailuresTotal, got)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("the entry was not persisted: %d rows", len(repo.rows))
	}
}

// Recording must not depend on a metrics recorder being present: auditing predates
// telemetry and must keep working when it is absent.
func TestAuditRecordWorksWithoutMetrics(t *testing.T) {
	repo := &fakeAuditRepo{}
	svc := &auditService{repo: repo}

	svc.Record(context.Background(), AuditEntry{Action: ActionLoginSuccess})

	if len(repo.rows) != 1 {
		t.Fatalf("a nil metrics recorder broke auditing: %d rows", len(repo.rows))
	}
}

// Retention counts what it removed, so trail shrinkage is attributable: rows vanishing
// without this counter moving did not come from retention.
func TestRetentionPurgeIsCounted(t *testing.T) {
	m := newCountingMetrics()
	repo := seedAuditRows(4, 1)
	svc := (&auditService{repo: repo}).WithMetrics(m)

	if _, err := svc.PurgeOlderThan(context.Background(), 30, t.TempDir()); err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}

	if got := m.get(MetricAuditRetentionPurgedTotal); got != 4 {
		t.Fatalf("%s = %v want 4", MetricAuditRetentionPurgedTotal, got)
	}
	// The purge writes its own audit entry, which must not be mistaken for a failure.
	if got := m.get(MetricAuditWriteFailuresTotal); got != 0 {
		t.Fatalf("%s = %v want 0", MetricAuditWriteFailuresTotal, got)
	}
}

// A purge that removed nothing must not move the counter, or "retention is deleting
// things" becomes indistinguishable from "retention ran".
func TestRetentionNoOpIsNotCounted(t *testing.T) {
	m := newCountingMetrics()
	svc := (&auditService{repo: seedAuditRows(0, 3)}).WithMetrics(m)

	if _, err := svc.PurgeOlderThan(context.Background(), 30, t.TempDir()); err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if got := m.get(MetricAuditRetentionPurgedTotal); got != 0 {
		t.Fatalf("%s = %v want 0 for a no-op run", MetricAuditRetentionPurgedTotal, got)
	}
}

// The gauge is polled rather than incremented at call sites, because sessions also end
// without anyone calling Revoke — a cache entry simply expires. A hand-maintained counter
// would drift from the truth and never recover; a periodic read of the index cannot.
func TestPublishActiveSessionsSetsTheGauge(t *testing.T) {
	m := newCountingMetrics()
	repo := &fakeSessionRepo{}
	now := time.Now().Unix()
	for i := 0; i < 3; i++ {
		if _, err := repo.Create(context.Background(), "", sharedentities.UserSession{
			UserLoginId: int64(i + 1),
			SessionId:   fmt.Sprintf("sid-%d", i),
			IsActive:    true,
			CreatedAt:   now,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// An ended session must not be counted.
	if _, err := repo.Create(context.Background(), "", sharedentities.UserSession{
		UserLoginId: 9, SessionId: "sid-dead", IsActive: false, RevokedAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := NewSessionService(repo, cache.NewMemoryStore(time.Minute, time.Minute), nil)
	if err := PublishActiveSessions(context.Background(), svc, m); err != nil {
		t.Fatalf("PublishActiveSessions: %v", err)
	}

	if got := m.gauge(MetricSessionsActive); got != 3 {
		t.Fatalf("%s = %v want 3 (a revoked session must not count)", MetricSessionsActive, got)
	}
}

// Telemetry can be disabled entirely, and the scheduler must not blow up when it is.
func TestPublishActiveSessionsNilSafe(t *testing.T) {
	if err := PublishActiveSessions(context.Background(), nil, nil); err != nil {
		t.Fatalf("nil inputs must be a no-op, got %v", err)
	}
}

// DescribeMetrics must be nil-safe: telemetry can be disabled entirely in config.
func TestDescribeMetricsNilSafe(t *testing.T) {
	DescribeMetrics(nil)
}
