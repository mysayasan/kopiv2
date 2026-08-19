package audit

import (
	"context"
	"errors"
	"sync"
	"testing"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// Test-local series names. The real ones are app-prefixed and passed in by each app's
// composition root (see MetricNames), so this package must not assume any of them —
// asserting against literals here is what proves the names really are a parameter.
const (
	testWriteFailuresMetric = "test_audit_write_failures_total"
	testPurgedMetric        = "test_audit_retention_purged_total"
)

// countingMetrics records Inc/Add calls by metric name.
type countingMetrics struct {
	telemetry.Metrics
	mu     sync.Mutex
	counts map[string]float64
}

func newCountingMetrics() *countingMetrics {
	return &countingMetrics{counts: map[string]float64{}}
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

func (m *countingMetrics) Set(string, telemetry.Labels, float64) {}

func (m *countingMetrics) get(name string) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counts[name]
}

// failingAuditRepo rejects every write.
type failingAuditRepo struct {
	dbsql.IGenericRepo[AuditLog]
}

func (failingAuditRepo) Create(context.Context, string, AuditLog) (uint64, error) {
	return 0, errors.New("disk full")
}

func (failingAuditRepo) Get(context.Context, string, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter) ([]*AuditLog, uint64, error) {
	return nil, 0, nil
}

func testMetricNames() MetricNames {
	return MetricNames{WriteFailuresTotal: testWriteFailuresMetric, RetentionPurgedTotal: testPurgedMetric}
}

// This counter is the reason the metric exists. Record() swallows its write error on
// purpose — auditing must never fail the action being audited — which means a trail that
// has stopped recording has NO other symptom. Every other signal stays green while the
// security history quietly develops a hole.
func TestAuditWriteFailureIsCounted(t *testing.T) {
	m := newCountingMetrics()
	svc := WithMetrics(NewService(failingAuditRepo{}, nil), m, testMetricNames())

	svc.Record(context.Background(), Entry{Action: "login.success", ActorEmail: "a@example.test"})
	svc.Record(context.Background(), Entry{Action: "login.failure", ActorEmail: "b@example.test"})

	if got := m.get(testWriteFailuresMetric); got != 2 {
		t.Fatalf("%s = %v want 2 — a trail that stopped recording would look identical to a healthy one", testWriteFailuresMetric, got)
	}
}

// The counter must stay at zero on the happy path, or it is useless as an alert.
func TestAuditWriteSuccessIsNotCounted(t *testing.T) {
	m := newCountingMetrics()
	repo := &fakeAuditRepo{}
	svc := WithMetrics(NewService(repo, nil), m, testMetricNames())

	svc.Record(context.Background(), Entry{Action: "login.success", ActorEmail: "a@example.test"})

	if got := m.get(testWriteFailuresMetric); got != 0 {
		t.Fatalf("%s = %v want 0 on a successful write", testWriteFailuresMetric, got)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("the entry was not persisted: %d rows", len(repo.rows))
	}
}

// Recording must not depend on a metrics recorder being present: auditing predates
// telemetry and must keep working when it is absent.
func TestAuditRecordWorksWithoutMetrics(t *testing.T) {
	repo := &fakeAuditRepo{}
	svc := NewService(repo, nil)

	svc.Record(context.Background(), Entry{Action: "login.success", ActorEmail: "a@example.test"})

	if len(repo.rows) != 1 {
		t.Fatalf("the entry was not persisted without metrics: %d rows", len(repo.rows))
	}
}

// A service configured with a recorder but NO series names must stay silent rather than
// reporting into an empty metric name — the names are per-app and an unconfigured app
// should get no series at all, not a nameless one.
func TestAuditMetricsSilentWithoutNames(t *testing.T) {
	m := newCountingMetrics()
	svc := WithMetrics(NewService(failingAuditRepo{}, nil), m, MetricNames{})

	svc.Record(context.Background(), Entry{Action: "login.success"})

	if got := m.get(""); got != 0 {
		t.Fatalf("reported into an empty metric name: %v", got)
	}
}

func TestRetentionPurgeIsCounted(t *testing.T) {
	m := newCountingMetrics()
	repo := seedAuditRows(4, 2)
	svc := WithMetrics(NewService(repo, nil), m, testMetricNames())

	if _, err := svc.PurgeOlderThan(context.Background(), 30, t.TempDir()); err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if got := m.get(testPurgedMetric); got != 4 {
		t.Fatalf("%s = %v want 4", testPurgedMetric, got)
	}
}

func TestRetentionNoOpIsNotCounted(t *testing.T) {
	m := newCountingMetrics()
	svc := WithMetrics(NewService(seedAuditRows(0, 3), nil), m, testMetricNames())

	if _, err := svc.PurgeOlderThan(context.Background(), 30, t.TempDir()); err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if got := m.get(testPurgedMetric); got != 0 {
		t.Fatalf("%s = %v want 0 when nothing expired", testPurgedMetric, got)
	}
}
