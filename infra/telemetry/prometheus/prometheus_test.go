package prometheus

import (
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/infra/telemetry"
)

func TestRecorderCollectsRequestAndSlowMetrics(t *testing.T) {
	rec := NewRecorder(Config{SlowThresholdMs: 100})
	rec.ObserveAPIRequest(telemetry.APIRequestMetric{
		AppName:    "mymatasan",
		Method:     "get",
		Path:       "/api/test/{id}",
		StatusCode: 200,
		DurationMs: 50,
	})
	rec.ObserveAPIRequest(telemetry.APIRequestMetric{
		AppName:    "mymatasan",
		Method:     "GET",
		Path:       "/api/test/{id}",
		StatusCode: 200,
		DurationMs: 150,
	})

	out := rec.Collect()
	if !strings.Contains(out, `kopiv2_api_requests_total{app="mymatasan",method="GET",path="/api/test/{id}",status="200"} 2`) {
		t.Fatalf("request total missing from output:\n%s", out)
	}
	if !strings.Contains(out, `kopiv2_api_slow_requests_total{app="mymatasan",method="GET",path="/api/test/{id}",status="200"} 1`) {
		t.Fatalf("slow request total missing from output:\n%s", out)
	}
	if !strings.Contains(out, `kopiv2_api_request_duration_ms_count{app="mymatasan",method="GET",path="/api/test/{id}",status="200"} 2`) {
		t.Fatalf("duration histogram count missing from output:\n%s", out)
	}
}

func TestRecorderCollectsCoordinationMetrics(t *testing.T) {
	rec := NewRecorder(Config{SlowThresholdMs: 100})
	rec.ObserveCoordination(telemetry.CoordinationMetric{
		AppName:  "mymatasan",
		Provider: "redis",
		Resource: "file-storage",
		Outcome:  "acquired",
		WaitMs:   15,
	})
	rec.ObserveCoordination(telemetry.CoordinationMetric{
		AppName:  "mymatasan",
		Provider: "redis",
		Resource: "file-storage",
		Outcome:  "stuck",
		WaitMs:   30000,
	})

	out := rec.Collect()
	if !strings.Contains(out, `kopiv2_tx_lock_events_total{app="mymatasan",provider="redis",resource="file-storage",outcome="acquired"} 1`) {
		t.Fatalf("coordination event total missing from output:\n%s", out)
	}
	if !strings.Contains(out, `kopiv2_tx_lock_wait_ms_count{app="mymatasan",provider="redis",resource="file-storage",outcome="acquired"} 1`) {
		t.Fatalf("coordination wait histogram missing from output:\n%s", out)
	}
	if !strings.Contains(out, `kopiv2_tx_lock_stuck_total{app="mymatasan",provider="redis",resource="file-storage",outcome="stuck"} 1`) {
		t.Fatalf("coordination stuck total missing from output:\n%s", out)
	}
}
