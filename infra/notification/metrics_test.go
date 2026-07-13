package notification

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/mysayasan/kopiv2/infra/telemetry"
)

type fakeMetrics struct {
	mu       sync.Mutex
	counters map[string]float64
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{counters: map[string]float64{}}
}

func (f *fakeMetrics) Describe(string, string) {}
func (f *fakeMetrics) Inc(name string, labels telemetry.Labels) {
	f.Add(name, labels, 1)
}
func (f *fakeMetrics) Add(name string, labels telemetry.Labels, delta float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counters[name+"|"+labels["channel"]+"|"+labels["outcome"]] += delta
}
func (f *fakeMetrics) Set(string, telemetry.Labels, float64)     {}
func (f *fakeMetrics) Observe(string, telemetry.Labels, float64) {}

func (f *fakeMetrics) get(key string) float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counters[key]
}

// Delivery is at-most-once: a full queue or exhausted retries drops the notification.
// Until now a drop was only a log line. Each outcome must be counted so "the alerts
// stopped" can be told apart from "the webhook is down".
func TestHub_CountsDeliveryOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		channel *fakeChannel
		outcome string
	}{
		{"success", &fakeChannel{}, "ok"},
		{"send error", &fakeChannel{err: errors.New("webhook 503")}, "failed"},
		{"panicking channel", &fakeChannel{panicMsg: "boom"}, "panic"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			metrics := newFakeMetrics()
			hub := NewHub()
			hub.SetMetrics(metrics)
			hub.Register(tc.channel)

			hub.Publish(context.Background(), Notification{Category: "test", Title: "t"})

			key := MetricDeliveryTotal + "|fake|" + tc.outcome
			if got := metrics.get(key); got != 1 {
				t.Fatalf("outcome %q was not counted (%s = %v)", tc.outcome, key, got)
			}
		})
	}
}

// Telemetry is optional — the hub must publish fine without it.
func TestHub_NilMetricsIsSafe(t *testing.T) {
	hub := NewHub()
	hub.Register(&fakeChannel{})
	hub.Publish(context.Background(), Notification{Category: "test", Title: "t"}) // must not panic
}
