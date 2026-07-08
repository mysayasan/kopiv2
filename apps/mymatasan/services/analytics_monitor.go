package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/domain/notification"
)

// anomalyCategory is the notification category for statistical anomaly alerts, so
// they flow into the unified feed and every configured destination (webhook /
// telegram / MQTT) and show up in the dashboard breakdowns like any other event.
const anomalyCategory = "analytics.anomaly"

// IAnomalyScanner scores a closed hour against per-camera baselines. The domain
// notification.Service satisfies it.
type IAnomalyScanner interface {
	AnomalyScan(ctx context.Context, hourStart, tzOffsetSec int64, k, minActivity float64) ([]notification.AnomalyFinding, error)
}

// AnalyticsMonitor is the statistical anomaly monitor: on an interval it scores the
// most recently CLOSED hour against each camera's learned (weekday, hour) baseline
// and raises notifications for spikes and "unusual silence". It mirrors the health
// monitors — settings are read live each tick, a per-(camera, direction) debounce +
// cooldown prevents flapping/spam, and it only scores whole, completed hours so an
// in-progress hour is never mistaken for a drop in activity.
type AnalyticsMonitor struct {
	scanner  IAnomalyScanner
	notifier INotificationPublisher
	settings IAnomalySettingsService
	camera   ICameraService

	mu           sync.Mutex
	lastEvalHour int64
	state        map[string]*anomalyState
}

// anomalyState is the per-(camera, direction) debounce: consecutive anomalous hours
// and the hour of the last alert (for cooldown).
type anomalyState struct {
	streak        int
	lastAlertHour int64
}

// NewAnalyticsMonitor builds the anomaly monitor. camera may be nil (names fall
// back to "Camera N"); scanner/notifier/settings are required.
func NewAnalyticsMonitor(scanner IAnomalyScanner, notifier INotificationPublisher, settings IAnomalySettingsService, camera ICameraService) *AnalyticsMonitor {
	return &AnalyticsMonitor{scanner: scanner, notifier: notifier, settings: settings, camera: camera, state: map[string]*anomalyState{}}
}

// Start launches the monitor loop in its own goroutine; it stops when ctx is done.
func (m *AnalyticsMonitor) Start(ctx context.Context) { go m.run(ctx) }

func (m *AnalyticsMonitor) run(ctx context.Context) {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			timer.Reset(m.tick(ctx))
		}
	}
}

func (m *AnalyticsMonitor) currentSettings(ctx context.Context) AnomalySettings {
	if m.settings != nil {
		if cfg, err := m.settings.Get(ctx); err == nil {
			return cfg
		}
	}
	return DefaultAnomalySettings()
}

// tick scores the most recently closed hour (once) and returns the delay until the
// next check.
func (m *AnalyticsMonitor) tick(ctx context.Context) time.Duration {
	cfg := m.currentSettings(ctx)
	interval := time.Duration(cfg.CheckIntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if !cfg.Enabled || m.scanner == nil {
		return interval
	}

	now := time.Now().UTC().Unix()
	closedHour := now - now%3600 - 3600 // the last whole, completed hour
	m.mu.Lock()
	already := closedHour <= m.lastEvalHour
	m.mu.Unlock()
	if already {
		return interval
	}

	// Baselines are seasonal by local clock; score in the server's local timezone.
	_, tzOffset := time.Now().Zone()

	findings, err := m.scanner.AnomalyScan(ctx, closedHour, int64(tzOffset), cfg.Sensitivity, cfg.MinActivity)
	if err != nil {
		return interval
	}

	seen := map[string]bool{}
	for _, f := range findings {
		if f.Direction == notification.AnomalyHigh && !cfg.DetectHigh {
			continue
		}
		if f.Direction == notification.AnomalyLow && !cfg.DetectLow {
			continue
		}
		key := fmt.Sprintf("%d:%s", f.CameraId, f.Direction)
		seen[key] = true

		m.mu.Lock()
		st := m.state[key]
		if st == nil {
			st = &anomalyState{}
			m.state[key] = st
		}
		st.streak++
		canAlert := st.streak >= cfg.RequireConsecutive &&
			(st.lastAlertHour == 0 || closedHour-st.lastAlertHour >= int64(cfg.CooldownHours)*3600)
		if canAlert {
			st.lastAlertHour = closedHour
		}
		m.mu.Unlock()

		if canAlert {
			m.emit(ctx, f)
		}
	}

	// Reset the streak for any camera+direction that was NOT anomalous this hour, so
	// RequireConsecutive counts truly consecutive hours.
	m.mu.Lock()
	for key, st := range m.state {
		if !seen[key] {
			st.streak = 0
		}
	}
	m.lastEvalHour = closedHour
	m.mu.Unlock()

	return interval
}

// emit publishes an anomaly notification for one finding.
func (m *AnalyticsMonitor) emit(ctx context.Context, f notification.AnomalyFinding) {
	if m.notifier == nil {
		return
	}
	name := m.cameraName(ctx, f.CameraId)
	var title, body string
	if f.Direction == notification.AnomalyHigh {
		title = "Unusual activity spike — " + name
		body = fmt.Sprintf("%s recorded %d events in the last hour, well above its usual %d–%d for this time.",
			name, f.Actual, roundI(f.Lo), roundI(f.Hi))
	} else {
		title = "Unusual quiet — " + name
		body = fmt.Sprintf("%s recorded only %d events in the last hour, below its usual %d–%d for this time — the camera may be blocked, offline, or tampered with.",
			name, f.Actual, roundI(f.Lo), roundI(f.Hi))
	}
	m.notifier.Publish(ctx, notification.Notification{
		Category: anomalyCategory,
		Severity: notification.Warning,
		Title:    title,
		Body:     body,
		Source:   "analytics-monitor",
		CameraId: f.CameraId,
		RefType:  "camera",
		RefId:    f.CameraId,
		Data: map[string]any{
			"direction":  f.Direction,
			"actual":     f.Actual,
			"expectedLo": f.Lo,
			"expectedHi": f.Hi,
			"median":     f.Mid,
			"hourStart":  f.HourStart,
			"samples":    f.Samples,
		},
	})
}

func (m *AnalyticsMonitor) cameraName(ctx context.Context, cameraId int64) string {
	if m.camera != nil {
		if n := m.camera.DisplayName(ctx, cameraId); n != "" {
			return n
		}
	}
	return fmt.Sprintf("Camera %d", cameraId)
}

func roundI(v float64) int64 {
	if v < 0 {
		return 0
	}
	return int64(v + 0.5)
}
