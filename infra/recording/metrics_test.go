package recording

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// fakeMetrics captures counter increments so a test can assert what the recorder
// actually reported, rather than that a method merely exists.
type fakeMetrics struct {
	mu       sync.Mutex
	counters map[string]float64
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{counters: map[string]float64{}}
}

func (f *fakeMetrics) key(name string, labels telemetry.Labels) string {
	k := name
	for _, l := range []string{"camera", "outcome"} {
		if v, ok := labels[l]; ok {
			k += "|" + l + "=" + v
		}
	}
	return k
}

func (f *fakeMetrics) Describe(string, string) {}
func (f *fakeMetrics) Inc(name string, labels telemetry.Labels) {
	f.Add(name, labels, 1)
}
func (f *fakeMetrics) Add(name string, labels telemetry.Labels, delta float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counters[f.key(name, labels)] += delta
}
func (f *fakeMetrics) Set(name string, labels telemetry.Labels, v float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counters[f.key(name, labels)] = v
}
func (f *fakeMetrics) Observe(string, telemetry.Labels, float64) {}

func (f *fakeMetrics) get(key string) float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counters[key]
}

// A finalize that fails must be counted. This is the metric that turns "footage is
// missing" from a support ticket into a number on a dashboard.
func TestMetrics_SegmentFinalizeOutcomeIsCounted(t *testing.T) {
	metrics := newFakeMetrics()
	r := newTestRecorder(t, &fakeSink{}, nil)
	r.cfg.Metrics = metrics

	// Both a .ts and an in-progress newer segment, with an unresolvable ffmpeg, so the
	// remux deterministically fails.
	writeSeg(t, r.liveDir, "20260101_000000.ts", []byte("payload"))
	writeSeg(t, r.liveDir, "20260101_001500.ts", []byte("in-progress"))

	r.saveCompletedSegments(context.Background())
	r.remuxWG.Wait()

	if got := metrics.get(MetricSegmentFinalizeTotal + "|camera=1|outcome=failed"); got != 1 {
		t.Fatalf("failed finalize was not counted: got %v", got)
	}
}

// The successful path must be counted too, or a dashboard cannot show a ratio.
func TestMetrics_SegmentAdoptionIsCountedAsSaved(t *testing.T) {
	metrics := newFakeMetrics()
	sink := &fakeSink{}
	r := newTestRecorder(t, sink, nil)
	r.cfg.Metrics = metrics

	writeSeg(t, r.liveDir, "20260101_000000.mp4", []byte("finalized segment"))
	writeSeg(t, r.liveDir, "20260101_001500.ts", []byte("in-progress"))

	r.saveCompletedSegments(context.Background())
	r.remuxWG.Wait()

	if got := metrics.get(MetricSegmentFinalizeTotal + "|camera=1|outcome=saved"); got != 1 {
		t.Fatalf("saved finalize was not counted: got %v", got)
	}
}

// A segment that exhausts its retries and is quarantined is the one recorder event worth
// paging someone about — it must be distinguishable from an ordinary transient failure.
func TestMetrics_QuarantineIsCountedSeparately(t *testing.T) {
	metrics := newFakeMetrics()
	r := newTestRecorder(t, &fakeSink{}, nil)
	r.cfg.Metrics = metrics

	tsPath := writeSeg(t, r.liveDir, "20260101_000000.ts", []byte("unreadable"))
	f := liveSegInfo{stem: "20260101_000000", tsPath: tsPath}

	for i := 0; i < maxRemuxAttempts; i++ {
		if !r.claimStem(f.stem) {
			t.Fatalf("claim %d should succeed", i)
		}
		r.finishStem(context.Background(), f, remuxFailed)
	}

	if got := metrics.get(MetricSegmentFinalizeTotal + "|camera=1|outcome=quarantined"); got != 1 {
		t.Fatalf("quarantine was not counted: got %v", got)
	}
	if _, err := filepath.Glob(filepath.Join(filepath.Dir(r.liveDir), "quarantine", "*.ts")); err != nil {
		t.Fatalf("glob: %v", err)
	}
}

// Telemetry is optional; the recorder must work with none.
func TestMetrics_NilMetricsIsSafe(t *testing.T) {
	r := newTestRecorder(t, &fakeSink{}, nil)
	r.cfg.Metrics = nil

	writeSeg(t, r.liveDir, "20260101_000000.ts", []byte("payload"))
	writeSeg(t, r.liveDir, "20260101_001500.ts", []byte("in-progress"))

	r.saveCompletedSegments(context.Background()) // must not panic
	r.remuxWG.Wait()
}
