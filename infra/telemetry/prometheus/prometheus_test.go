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
