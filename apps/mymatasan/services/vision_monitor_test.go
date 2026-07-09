package services

import (
	"testing"
	"time"
)

// TestSampleIntervalFromSettings locks the live detector-rate throttle mapping: the
// runtime "Interval (ms)" capture setting drives the per-camera sampling cadence, with
// 0 falling back to the built-in interval and out-of-range values clamped to [250ms,60s].
func TestSampleIntervalFromSettings(t *testing.T) {
	m := &VisionMonitor{interval: 2 * time.Second}
	cases := []struct {
		name     string
		ms       int
		expected time.Duration
	}{
		{"zero falls back to built-in", 0, 2 * time.Second},
		{"negative falls back", -100, 2 * time.Second},
		{"normal value honored", 5000, 5 * time.Second},
		{"below floor clamps to 250ms", 100, 250 * time.Millisecond},
		{"above ceiling clamps to 60s", 120000, 60 * time.Second},
	}
	for _, c := range cases {
		if got := m.sampleIntervalFromSettings(c.ms); got != c.expected {
			t.Errorf("%s: sampleIntervalFromSettings(%d) = %v, want %v", c.name, c.ms, got, c.expected)
		}
	}
}
