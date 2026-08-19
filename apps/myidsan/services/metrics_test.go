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

// The audit metric BEHAVIOUR (failures counted, successes not, retention counted, safe
// without a recorder) moved to domain/shared/audit alongside the implementation. What
// belongs here is the part that is myidsan's: that WithAuditMetrics wires myidsan's own
// series names, so an existing dashboard or alert keeps receiving the series it watches.
func TestWithAuditMetricsUsesMyidsanSeriesNames(t *testing.T) {
	m := newCountingMetrics()
	svc := WithAuditMetrics(NewAuditService(failingAuditRepo{}, nil), m)

	svc.Record(context.Background(), AuditEntry{Action: ActionLoginSuccess, ActorEmail: "a@example.test"})

	if got := m.get(MetricAuditWriteFailuresTotal); got != 1 {
		t.Fatalf("%s = %v want 1 — the shared trail was wired with the wrong series name", MetricAuditWriteFailuresTotal, got)
	}
}

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
