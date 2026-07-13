package prometheus

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/mysayasan/kopiv2/infra/telemetry"
)

func newTestRecorder() *Recorder {
	return NewRecorder(Config{})
}

func TestMetrics_CounterGaugeHistogramExposition(t *testing.T) {
	r := newTestRecorder()
	r.Describe("mymatasan_ffmpeg_restarts_total", "ffmpeg restarts")

	r.Inc("mymatasan_ffmpeg_restarts_total", telemetry.Labels{"camera": "3"})
	r.Inc("mymatasan_ffmpeg_restarts_total", telemetry.Labels{"camera": "3"})
	r.Set("mymatasan_cameras_offline", telemetry.Labels{}, 2)
	r.Observe("mymatasan_inference_duration_ms", telemetry.Labels{"camera": "3"}, 40)

	out := r.Collect()

	for _, want := range []string{
		"# HELP mymatasan_ffmpeg_restarts_total ffmpeg restarts",
		"# TYPE mymatasan_ffmpeg_restarts_total counter",
		`mymatasan_ffmpeg_restarts_total{camera="3"} 2`,
		"# TYPE mymatasan_cameras_offline gauge",
		"mymatasan_cameras_offline 2",
		"# TYPE mymatasan_inference_duration_ms histogram",
		`mymatasan_inference_duration_ms_bucket{camera="3",le="50"} 1`,
		`mymatasan_inference_duration_ms_bucket{camera="3",le="25"} 0`,
		`mymatasan_inference_duration_ms_count{camera="3"} 1`,
		`mymatasan_inference_duration_ms_sum{camera="3"} 40`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("scrape is missing %q\n---\n%s", want, out)
		}
	}
}

// A metric name cannot be both a counter and a gauge in Prometheus — a conflicting
// observation must be dropped, not allowed to corrupt the exposition.
func TestMetrics_KindConflictIsDropped(t *testing.T) {
	r := newTestRecorder()
	r.Inc("thing", telemetry.Labels{})
	r.Set("thing", telemetry.Labels{}, 99) // wrong kind for this name

	out := r.Collect()
	if !strings.Contains(out, "# TYPE thing counter") {
		t.Fatalf("counter kind should have been kept:\n%s", out)
	}
	if strings.Contains(out, "thing 99") {
		t.Fatalf("a conflicting-kind observation was applied:\n%s", out)
	}
}

// This box runs for months. An accidentally unbounded label must not grow the process
// without limit, and the truncation must be VISIBLE — a quietly short metric is worse
// than a loud one.
func TestMetrics_CardinalityIsCappedAndTruncationIsVisible(t *testing.T) {
	r := newTestRecorder()
	for i := 0; i < maxSeriesPerMetric+50; i++ {
		r.Inc("leaky", telemetry.Labels{"id": fmt.Sprintf("%d", i)})
	}

	r.mu.Lock()
	got := len(r.custom["leaky"].series)
	r.mu.Unlock()
	if got != maxSeriesPerMetric {
		t.Fatalf("series = %d, want the cap %d", got, maxSeriesPerMetric)
	}

	out := r.Collect()
	if !strings.Contains(out, "leaky_series_truncated 1") {
		t.Fatalf("truncation was not surfaced in the scrape:\n%s", out[:min(len(out), 400)])
	}
}

// Label maps have no order; the same set must always resolve to the same series.
func TestMetrics_LabelOrderDoesNotSplitSeries(t *testing.T) {
	r := newTestRecorder()
	r.Inc("x", telemetry.Labels{"a": "1", "b": "2"})
	r.Inc("x", telemetry.Labels{"b": "2", "a": "1"})

	r.mu.Lock()
	got := len(r.custom["x"].series)
	r.mu.Unlock()
	if got != 1 {
		t.Fatalf("same label set produced %d series, want 1", got)
	}
	if !strings.Contains(r.Collect(), `x{a="1",b="2"} 2`) {
		t.Fatalf("expected a single series with value 2:\n%s", r.Collect())
	}
}

// Callers mutate/reuse label maps; the recorder must not alias one.
func TestMetrics_LabelsAreCopied(t *testing.T) {
	r := newTestRecorder()
	labels := telemetry.Labels{"camera": "1"}
	r.Inc("y", labels)
	labels["camera"] = "999" // caller reuses the map

	if !strings.Contains(r.Collect(), `y{camera="1"} 1`) {
		t.Fatalf("recorder aliased the caller's label map:\n%s", r.Collect())
	}
}

// These are called from per-frame and per-segment paths; concurrent use must be safe.
func TestMetrics_ConcurrentObservations(t *testing.T) {
	r := newTestRecorder()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				r.Inc("hits", telemetry.Labels{"camera": "1"})
				r.Observe("dur", telemetry.Labels{"camera": "1"}, 5)
			}
		}()
	}
	wg.Wait()

	if !strings.Contains(r.Collect(), `hits{camera="1"} 1000`) {
		t.Fatalf("lost counter increments under concurrency:\n%s", r.Collect())
	}
}

// Telemetry can be disabled; instrumentation call sites must still be safe.
func TestNoopRecorder_ImplementsMetrics(t *testing.T) {
	var m telemetry.Metrics = telemetry.NewNoopRecorder()
	m.Describe("a", "b")
	m.Inc("a", nil)
	m.Add("a", nil, 2)
	m.Set("a", nil, 3)
	m.Observe("a", nil, 4)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
