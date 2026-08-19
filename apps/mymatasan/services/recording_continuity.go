package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/domain/notification"
	"github.com/mysayasan/kopiv2/infra/safego"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// The recording-continuity monitor answers the one question an NVR exists for and that
// nothing in this product could answer: was there actually footage?
//
// CameraHealthMonitor probes reachability. It is well built and it answers a different
// question — a camera can be perfectly reachable while ffmpeg is wedged, the disk is
// full, the remux queue is quarantining every segment, or the stream URL silently
// changed. Every one of those records nothing and reports green, and the operator finds
// out at the worst possible moment: when somebody asks for footage of an incident.
//
// The data was already there. RecordingSegment carries StartedAt/EndedAt under a
// cam_time composite index, and RecordingConfig.Enabled says which cameras are supposed
// to be recording. The monitor is a query, not a new pipeline.

// continuityCategory is the notification category. It rides the health-check category
// rather than a new one so it lands in the same feed, breakdowns and destinations an
// operator already watches for "this camera has a problem".
const continuityCategory = notification.CategoryHealthCheck

// MetricRecordingCoveragePercent is last-scored-hour coverage per camera, and
// MetricRecordingGapCameras is how many cameras are currently in a continuity alert —
// the single number worth alerting on.
const (
	MetricRecordingCoveragePercent = "mymatasan_recording_coverage_percent"
	MetricRecordingGapCameras      = "mymatasan_recording_gap_cameras"
)

// recorderPauseState reports whether the disk guard has recording paused. Narrow on
// purpose: the monitor needs one bool from the recorder and nothing else.
type recorderPauseState interface {
	IsPaused() bool
}

// RecordingContinuityMonitor scores each closed hour of every recording-enabled camera
// against its expected coverage and raises an edge-triggered alert when footage is
// missing. It mirrors CameraHealthMonitor: settings read live each sweep, per-camera
// debounce, and a notification only on a transition.
type RecordingContinuityMonitor struct {
	recording IRecordingService
	camera    ICameraService
	settings  IContinuitySettingsService
	notifier  INotificationPublisher
	recorder  recorderPauseState
	metrics   telemetry.Metrics

	mu    sync.Mutex
	state map[int64]*continuityState

	// now is injected so the tests can score a specific hour rather than sleeping.
	now func() time.Time
}

// continuityState is the per-camera debounce.
type continuityState struct {
	// alerting is whether an alert is currently raised.
	alerting bool
	// badStreak / goodStreak count consecutive scored hours either side of the threshold.
	badStreak  int
	goodStreak int
	// lastScoredHour is the hour start this camera was last scored for, so a sweep that
	// runs more often than hourly does not score the same hour repeatedly (which would
	// drive the streaks up and fire on the first bad hour).
	lastScoredHour int64
}

func NewRecordingContinuityMonitor(
	recording IRecordingService,
	camera ICameraService,
	settings IContinuitySettingsService,
	notifier INotificationPublisher,
	recorder recorderPauseState,
	metrics telemetry.Metrics,
) *RecordingContinuityMonitor {
	return &RecordingContinuityMonitor{
		recording: recording,
		camera:    camera,
		settings:  settings,
		notifier:  notifier,
		recorder:  recorder,
		metrics:   metrics,
		state:     map[int64]*continuityState{},
		now:       func() time.Time { return time.Now() },
	}
}

func (m *RecordingContinuityMonitor) Start(ctx context.Context) {
	safego.Supervise(ctx, "mymatasan.health.continuity", m.run)
}

func (m *RecordingContinuityMonitor) run(ctx context.Context) {
	// The first sweep is delayed: at boot the recorders have not started yet, and the
	// previous hour was recorded by the previous process — scoring it immediately would
	// alert on an hour this instance was never responsible for.
	timer := time.NewTimer(2 * time.Minute)
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

func (m *RecordingContinuityMonitor) tick(ctx context.Context) time.Duration {
	cfg := m.currentSettings(ctx)
	interval := time.Duration(cfg.IntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	if !cfg.Enabled {
		return interval
	}
	m.Sweep(ctx, cfg)
	return interval
}

// Sweep scores the most recently closed hour for every recording-enabled camera. Exported
// so a test can drive one sweep without a timer.
func (m *RecordingContinuityMonitor) Sweep(ctx context.Context, cfg ContinuitySettings) {
	// Only whole, CLOSED hours are scored. An hour still in progress is legitimately
	// under-covered for the whole of it, and scoring it would raise a gap every sweep —
	// the same trap AnalyticsMonitor documents for its baselines.
	hourStart := m.now().UTC().Truncate(time.Hour).Add(-time.Hour).Unix()
	hourEnd := hourStart + 3600

	configs, err := m.recording.ListConfigs(ctx)
	if err != nil {
		return
	}

	// A paused recorder is a KNOWN reason for a gap: the machine-health disk guard stops
	// every recorder when the volume is nearly full, and it already raises its own alert.
	// Scoring through a pause would blame every camera for one disk problem.
	paused := m.recorder != nil && m.recorder.IsPaused()

	gapCount := 0
	for _, cfg2 := range configs {
		if cfg2 == nil || !cfg2.Enabled {
			// Not supposed to be recording. This is also what excludes detect-only AI
			// frame sources: EnsureDetectionStream builds its own config and never sets
			// Enabled on the stored row, so a detect-only camera is correctly absent here
			// rather than being blamed for writing no segments by design.
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if m.scoreCamera(ctx, cfg2.CameraId, hourStart, hourEnd, cfg, paused) {
			gapCount++
		}
	}
	if m.metrics != nil {
		m.metrics.Set(MetricRecordingGapCameras, nil, float64(gapCount))
	}
}

// scoreCamera scores one camera's hour and returns whether it is currently alerting.
func (m *RecordingContinuityMonitor) scoreCamera(ctx context.Context, cameraId, hourStart, hourEnd int64, cfg ContinuitySettings, paused bool) bool {
	report, err := m.recording.Coverage(ctx, cameraId, hourStart, hourEnd, "hour")
	if err != nil {
		return false
	}
	pct := report.OverallPercent
	if m.metrics != nil {
		m.metrics.Set(MetricRecordingCoveragePercent, telemetry.Labels{"camera": fmt.Sprint(cameraId)}, pct)
	}

	m.mu.Lock()
	st := m.state[cameraId]
	if st == nil {
		st = &continuityState{}
		m.state[cameraId] = st
	}
	// Score each hour ONCE. Without this a 10-minute sweep interval scores the same
	// closed hour six times, driving the bad streak past any threshold and turning a
	// debounce meant to span hours into one that fires within the first.
	if st.lastScoredHour == hourStart {
		alerting := st.alerting
		m.mu.Unlock()
		return alerting
	}
	st.lastScoredHour = hourStart

	transition := ""
	if pct < cfg.MinCoveragePercent {
		st.goodStreak = 0
		st.badStreak++
		if !st.alerting && st.badStreak >= cfg.FailureThreshold {
			st.alerting = true
			transition = "gap"
		}
	} else {
		st.badStreak = 0
		st.goodStreak++
		if st.alerting && st.goodStreak >= cfg.RecoveryThreshold {
			st.alerting = false
			transition = "recovered"
		}
	}
	alerting := st.alerting
	m.mu.Unlock()

	if transition == "" {
		return alerting
	}
	// A pause suppresses the ALERT but not the scoring above, so the streak still
	// reflects reality and a camera that was already failing before the pause does not
	// get its history reset by it.
	if transition == "gap" && paused {
		return alerting
	}
	m.publish(ctx, cameraId, transition, pct, hourStart, hourEnd)
	return alerting
}

// publish raises the transition notification.
func (m *RecordingContinuityMonitor) publish(ctx context.Context, cameraId int64, transition string, pct float64, hourStart, hourEnd int64) {
	if m.notifier == nil {
		return
	}
	name := fmt.Sprintf("Camera %d", cameraId)
	if m.camera != nil {
		if n := strings.TrimSpace(m.camera.DisplayName(ctx, cameraId)); n != "" {
			name = n
		}
	}
	window := fmt.Sprintf("%s–%s UTC",
		time.Unix(hourStart, 0).UTC().Format("15:04"),
		time.Unix(hourEnd, 0).UTC().Format("15:04"))

	if transition == "recovered" {
		m.notifier.Publish(ctx, notification.Notification{
			Category: continuityCategory,
			Severity: notification.Info,
			Title:    "Recording resumed",
			Body:     fmt.Sprintf("%s is recording normally again (%s: %.0f%% covered)", name, window, pct),
			Source:   "recording-continuity-monitor",
			CameraId: cameraId,
			RefType:  "camera",
			RefId:    cameraId,
			Data: map[string]any{
				"cameraId": cameraId, "cameraName": name, "status": "covered",
				"coveragePercent": pct, "windowStart": hourStart, "windowEnd": hourEnd,
			},
		})
		return
	}

	// Attribute the gap where the cause is already known. One incident should read as one
	// story: if the reachability monitor has this camera offline, "no footage" is that
	// outage's consequence, not a second independent fault to chase.
	reason := "unexplained"
	detail := ""
	if m.camera != nil {
		if cam, err := m.camera.GetById(ctx, uint64(cameraId)); err == nil && cam != nil &&
			strings.EqualFold(strings.TrimSpace(cam.HealthStatus), cameraHealthOffline) {
			reason = "camera-offline"
			detail = " — the camera was offline"
		}
	}

	m.notifier.Publish(ctx, notification.Notification{
		Category: continuityCategory,
		Severity: notification.Critical,
		Title:    "Recording gap",
		Body: fmt.Sprintf("%s recorded only %.0f%% of %s%s",
			name, pct, window, detail),
		Source:   "recording-continuity-monitor",
		CameraId: cameraId,
		RefType:  "camera",
		RefId:    cameraId,
		Data: map[string]any{
			"cameraId": cameraId, "cameraName": name, "status": "gap",
			"coveragePercent": pct, "windowStart": hourStart, "windowEnd": hourEnd,
			"reason": reason,
		},
	})
}

func (m *RecordingContinuityMonitor) currentSettings(ctx context.Context) ContinuitySettings {
	if m.settings == nil {
		return DefaultContinuitySettings()
	}
	cfg, err := m.settings.Get(ctx)
	if err != nil {
		return DefaultContinuitySettings()
	}
	return cfg
}
