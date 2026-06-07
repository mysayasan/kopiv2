package prometheus

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// Config controls the Prometheus telemetry recorder.
type Config struct {
	SlowThresholdMs int64
}

type metricKey struct {
	AppName    string
	Method     string
	Path       string
	StatusCode int
}

type series struct {
	RequestsTotal uint64
	DurationSum   float64
	Buckets       []uint64
	SlowTotal     uint64
	SlowSum       float64
	SlowBuckets   []uint64
}

// Recorder stores API request metrics and exposes them in Prometheus text format.
type Recorder struct {
	mu              sync.Mutex
	slowThresholdMs int64
	buckets         []float64
	series          map[metricKey]*series
}

// NewRecorder creates a Prometheus telemetry recorder.
func NewRecorder(cfg Config) *Recorder {
	if cfg.SlowThresholdMs < 0 {
		cfg.SlowThresholdMs = 0
	}
	return &Recorder{
		slowThresholdMs: cfg.SlowThresholdMs,
		buckets:         []float64{10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000},
		series:          map[metricKey]*series{},
	}
}

// ObserveAPIRequest records a completed API request duration and slow request count.
func (r *Recorder) ObserveAPIRequest(metric telemetry.APIRequestMetric) {
	if r == nil {
		return
	}

	duration := float64(metric.DurationMs)
	key := metricKey{
		AppName:    strings.TrimSpace(metric.AppName),
		Method:     strings.ToUpper(strings.TrimSpace(metric.Method)),
		Path:       strings.TrimSpace(metric.Path),
		StatusCode: metric.StatusCode,
	}
	if key.AppName == "" {
		key.AppName = "unknown"
	}
	if key.Method == "" {
		key.Method = "UNKNOWN"
	}
	if key.Path == "" {
		key.Path = "unknown"
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	entry := r.series[key]
	if entry == nil {
		entry = &series{
			Buckets:     make([]uint64, len(r.buckets)),
			SlowBuckets: make([]uint64, len(r.buckets)),
		}
		r.series[key] = entry
	}

	entry.RequestsTotal++
	entry.DurationSum += duration
	for i, bucket := range r.buckets {
		if duration <= bucket {
			entry.Buckets[i]++
		}
	}

	if r.slowThresholdMs > 0 && metric.DurationMs >= r.slowThresholdMs {
		entry.SlowTotal++
		entry.SlowSum += duration
		for i, bucket := range r.buckets {
			if duration <= bucket {
				entry.SlowBuckets[i]++
			}
		}
	}
}

// Handler returns an HTTP handler for Prometheus scrapes.
func (r *Recorder) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.Collect()))
	})
}

// Collect returns all metrics in Prometheus text exposition format.
func (r *Recorder) Collect() string {
	if r == nil {
		return ""
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	keys := make([]metricKey, 0, len(r.series))
	for key := range r.series {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].sortKey() < keys[j].sortKey()
	})

	var b strings.Builder
	b.WriteString("# HELP kopiv2_api_requests_total Total API requests observed.\n")
	b.WriteString("# TYPE kopiv2_api_requests_total counter\n")
	for _, key := range keys {
		entry := r.series[key]
		fmt.Fprintf(&b, "kopiv2_api_requests_total%s %d\n", labels(key, ""), entry.RequestsTotal)
	}

	b.WriteString("# HELP kopiv2_api_request_duration_ms API request duration in milliseconds.\n")
	b.WriteString("# TYPE kopiv2_api_request_duration_ms histogram\n")
	for _, key := range keys {
		entry := r.series[key]
		writeHistogram(&b, "kopiv2_api_request_duration_ms", key, r.buckets, entry.Buckets, entry.RequestsTotal, entry.DurationSum)
	}

	b.WriteString("# HELP kopiv2_api_slow_requests_total API requests at or above the configured slow threshold.\n")
	b.WriteString("# TYPE kopiv2_api_slow_requests_total counter\n")
	for _, key := range keys {
		entry := r.series[key]
		fmt.Fprintf(&b, "kopiv2_api_slow_requests_total%s %d\n", labels(key, ""), entry.SlowTotal)
	}

	b.WriteString("# HELP kopiv2_api_slow_request_duration_ms Slow API request duration in milliseconds.\n")
	b.WriteString("# TYPE kopiv2_api_slow_request_duration_ms histogram\n")
	for _, key := range keys {
		entry := r.series[key]
		writeHistogram(&b, "kopiv2_api_slow_request_duration_ms", key, r.buckets, entry.SlowBuckets, entry.SlowTotal, entry.SlowSum)
	}

	return b.String()
}

func writeHistogram(b *strings.Builder, name string, key metricKey, buckets []float64, counts []uint64, total uint64, sum float64) {
	for i, bucket := range buckets {
		fmt.Fprintf(b, "%s%s %d\n", name+"_bucket", labels(key, formatBucket(bucket)), counts[i])
	}
	fmt.Fprintf(b, "%s%s %d\n", name+"_bucket", labels(key, "+Inf"), total)
	fmt.Fprintf(b, "%s%s %.0f\n", name+"_sum", labels(key, ""), sum)
	fmt.Fprintf(b, "%s%s %d\n", name+"_count", labels(key, ""), total)
}

func labels(key metricKey, le string) string {
	parts := []string{
		`app="` + escapeLabel(key.AppName) + `"`,
		`method="` + escapeLabel(key.Method) + `"`,
		`path="` + escapeLabel(key.Path) + `"`,
		`status="` + strconv.Itoa(key.StatusCode) + `"`,
	}
	if le != "" {
		parts = append(parts, `le="`+escapeLabel(le)+`"`)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func formatBucket(bucket float64) string {
	if bucket == float64(int64(bucket)) {
		return strconv.FormatInt(int64(bucket), 10)
	}
	return strconv.FormatFloat(bucket, 'f', -1, 64)
}

func (k metricKey) sortKey() string {
	return k.AppName + "\x00" + k.Method + "\x00" + k.Path + "\x00" + strconv.Itoa(k.StatusCode)
}
