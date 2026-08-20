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
	"github.com/mysayasan/kopiv2/infra/vision"
)

// The camera tamper monitor: is this camera still showing what it is supposed to show?
//
// It is the third of three questions, and the other two both answer "yes" while it fails.
// CameraHealthMonitor asks whether the camera answers — a covered lens answers fine.
// RecordingContinuityMonitor asks whether footage is being written — footage of a wall is
// still footage. Only this one notices that somebody put a bag over the dome, turned it
// to face the ceiling, or pointed a laser at it, which is what an attacker does BEFORE
// anything worth recording happens.
//
// It needs no ML and no detector: it reads the JPEG the recorder already siphons for the
// AI pipeline and runs a few hundred lines of arithmetic (infra/vision/tamper.go).

// tamperCategory rides the health-check category so tamper alerts land in the same feed,
// breakdowns and destinations an operator already watches for "this camera has a problem".
const tamperCategory = notification.CategoryHealthCheck

// Tamper kinds, used as the notification subtype and the metric label.
const (
	TamperFrozen  = "frozen"
	TamperCovered = "covered"
	TamperMoved   = "moved"
)

// MetricCameraTamperTotal counts raised tamper alerts by kind.
const MetricCameraTamperTotal = "mymatasan_camera_tamper_total"

// tamperBaselineSize is how many recent readings describe a camera's normal picture.
//
// A per-camera, rolling baseline is what makes "covered" workable at all. An absolute
// edge-energy threshold cannot be right for both a busy loading bay and a plain corridor,
// and it cannot be right for the same camera at noon and at dusk. A median over recent
// samples tracks the gradual change of daylight while a hand over the lens is a step
// change away from it.
const tamperBaselineSize = 30

// frameSource is the slice of the recorder the monitor needs: the latest siphoned frame.
type frameSource interface {
	LatestFrame(cameraId int64) ([]byte, int64, bool)
}

// CameraTamperMonitor samples each recording camera's latest frame and raises an
// edge-triggered alert when the view stops being the view. It mirrors the other health
// monitors: settings read live each sweep, per-camera debounce, notification on
// transitions only.
type CameraTamperMonitor struct {
	frames    frameSource
	camera    ICameraService
	settings  ITamperSettingsService
	notifier  INotificationPublisher
	recording IRecordingService
	metrics   telemetry.Metrics

	mu    sync.Mutex
	state map[int64]*tamperState
}

// tamperState is one camera's rolling reference and debounce.
type tamperState struct {
	last *vision.Fingerprint
	// lastCapturedAt is the timestamp of the frame `last` came from, used to tell a
	// genuinely frozen stream from a siphon that simply has not produced a new frame yet.
	lastCapturedAt int64
	// edges is the rolling window of recent edge-energy readings; its median is the
	// camera's "normal".
	edges []float64
	// hists is the rolling window of recent luma histograms; its per-bucket median is
	// what this camera's picture normally looks like, and MOVED is measured against it.
	//
	// It exists for the same reason edges does. The alternative — and what this monitor
	// originally did — was to compare each sample against the one before it, which cannot
	// work: a camera that is re-aimed differs from its predecessor for exactly ONE sample
	// and then matches it again, because the new view is as steady as the old one. The
	// debounce below then resets the streak on the very next sample and the verdict can
	// never be reached. See the moved block in sample().
	hists [][]float64
	// streak counts consecutive abnormal samples per kind; active records which kinds
	// are currently alerting, so an alert is raised and cleared once rather than repeated.
	streak map[string]int
	active map[string]bool
	// frozenSince is when the picture first stopped changing, so the frozen rule can be
	// expressed in SECONDS rather than in samples — a static scene at 3am is normal and
	// only a long-enough stillness is evidence of a stopped stream.
	frozenSince int64
}

func NewCameraTamperMonitor(
	frames frameSource,
	camera ICameraService,
	recording IRecordingService,
	settings ITamperSettingsService,
	notifier INotificationPublisher,
	metrics telemetry.Metrics,
) *CameraTamperMonitor {
	return &CameraTamperMonitor{
		frames: frames, camera: camera, recording: recording,
		settings: settings, notifier: notifier, metrics: metrics,
		state: map[int64]*tamperState{},
	}
}

func (m *CameraTamperMonitor) Start(ctx context.Context) {
	safego.Supervise(ctx, "mymatasan.health.tamper", m.run)
}

func (m *CameraTamperMonitor) run(ctx context.Context) {
	// Delayed first sweep: at boot the recorders have not produced a siphon frame yet,
	// and a camera with no frame must not read as a fault.
	timer := time.NewTimer(90 * time.Second)
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

func (m *CameraTamperMonitor) tick(ctx context.Context) time.Duration {
	cfg := m.currentSettings(ctx)
	interval := time.Duration(cfg.IntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if !cfg.Enabled {
		return interval
	}
	m.Sweep(ctx, cfg, time.Now().UTC().Unix())
	return interval
}

// Sweep samples every camera once. Exported, with the clock passed in, so a test can run
// a sequence of sweeps without sleeping.
func (m *CameraTamperMonitor) Sweep(ctx context.Context, cfg TamperSettings, now int64) {
	configs, err := m.recording.ListConfigs(ctx)
	if err != nil {
		return
	}
	for _, rc := range configs {
		// Only cameras with a live stream have a siphon frame to read. A camera whose
		// recording is off is not being watched by this monitor, and saying so is more
		// honest than reporting it as healthy.
		if rc == nil || !rc.Enabled {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		m.sampleCamera(ctx, rc.CameraId, cfg, now)
	}
}

func (m *CameraTamperMonitor) sampleCamera(ctx context.Context, cameraId int64, cfg TamperSettings, now int64) {
	frame, capturedAt, ok := m.frames.LatestFrame(cameraId)
	if !ok || len(frame) == 0 {
		return
	}
	fp, err := vision.NewFingerprint(frame)
	if err != nil {
		return
	}

	m.mu.Lock()
	st := m.state[cameraId]
	if st == nil {
		st = &tamperState{streak: map[string]int{}, active: map[string]bool{}}
		m.state[cameraId] = st
	}
	prev, prevAt := st.last, st.lastCapturedAt
	baseline := vision.Median(st.edges)
	haveBaseline := len(st.edges) >= tamperBaselineSize/2
	// Both references describe what came BEFORE this sample, and are read before it is
	// folded in — otherwise "is this frame different from this camera's normal" is answered
	// partly with the frame itself, which drags the answer toward "no" exactly when it
	// should be "yes".
	reference := vision.MedianHistogram(st.hists)
	haveReference := len(st.hists) >= tamperBaselineSize/2

	// The baseline records what this camera normally looks like, so a reading taken
	// while it is ALREADY alerting must not be folded into it — otherwise a lens left
	// covered for an hour quietly becomes the new normal and the alert clears itself.
	if !st.active[TamperCovered] {
		st.edges = append(st.edges, fp.EdgeEnergy)
		if len(st.edges) > tamperBaselineSize {
			st.edges = st.edges[len(st.edges)-tamperBaselineSize:]
		}
	}
	// The same exclusion, for the same reason: a camera left pointing at a wall must not
	// have the wall folded into its idea of normal, or the alert clears itself after half
	// a window and the camera is quietly accepted where it now points.
	//
	// Frames taken while the lens is COVERED are excluded too, which the edge-energy
	// baseline does not need to do for itself but this one does. A lens covered for an hour
	// would otherwise fill the reference with featureless grey, and the moment somebody
	// uncovered it the real scene would be a long way from that reference — so clearing one
	// alarm would immediately raise another, blaming an operator for moving a camera they
	// had just fixed.
	if !st.active[TamperMoved] && !st.active[TamperCovered] {
		st.hists = append(st.hists, append([]float64(nil), fp.Histogram...))
		if len(st.hists) > tamperBaselineSize {
			st.hists = st.hists[len(st.hists)-tamperBaselineSize:]
		}
	}

	st.last, st.lastCapturedAt = fp, capturedAt

	verdicts := map[string]bool{}

	// FROZEN. Judged on the picture not changing AT ALL between two frames the siphon
	// says are different frames. Even an empty room at night has sensor noise, so an
	// exact zero is a stopped source rather than a calm scene. It still has to persist:
	// the rule is expressed in seconds, because a few identical frames can happen and an
	// hour of them cannot.
	if prev != nil && capturedAt > prevAt {
		if vision.FrameDifference(prev, fp) <= cfg.FrozenMaxDifference {
			if st.frozenSince == 0 {
				st.frozenSince = now
			}
		} else {
			st.frozenSince = 0
		}
	}
	if st.frozenSince > 0 && now-st.frozenSince >= int64(cfg.FrozenSeconds) {
		verdicts[TamperFrozen] = true
	}

	// COVERED. A collapse in edge energy relative to THIS camera's recent normal.
	//
	// Suppressed entirely in low light: a dark scene loses its edge energy legitimately,
	// and under infrared it loses contrast as well. Without this, every camera in the
	// fleet reports a covered lens at dusk and the feature is muted by morning — after
	// which it protects nothing. A lens covered overnight is missed; that is the right
	// trade against a monitor nobody trusts.
	if haveBaseline && baseline > 0 && !vision.LowLight(fp) {
		if fp.EdgeEnergy < baseline*cfg.CoveredRatio {
			verdicts[TamperCovered] = true
		}
	}

	// MOVED. A large, sustained shift in the whole brightness distribution, measured
	// against what this camera NORMALLY looks like. Position-blind on purpose: a person
	// crossing frame barely moves the histogram, a camera turned to face a wall changes
	// all of it, and the debounce below separates the two.
	//
	// Against the ROLLING REFERENCE, not against the previous sample. Comparing adjacent
	// samples measures a transient — a re-aimed camera differs from its predecessor once
	// and then never again — while settle() requires the verdict to hold for
	// FailureThreshold samples running. The two are incompatible, and the result was a
	// verdict that could not fire at all: benching it live produced covered and frozen
	// alerts and never a single moved one, and no test in the suite drove it to an alert,
	// so a green suite said nothing about it.
	//
	// Suppressed in low light, exactly as covered is. A camera whose scene goes dark has
	// lost its histogram legitimately, and a fleet that reports every camera as MOVED at
	// dusk is a fleet whose tamper alerts get muted — after which none of them protect
	// anything.
	//
	// Not judged at all while the lens is covered. A bag over the lens changes the whole
	// histogram too, so without this one physical event raises two alarms that ask for
	// different actions — "someone blinded this camera" and "this camera is pointing
	// somewhere else" — and the second one is not something the picture can support: you
	// cannot tell where a camera is aimed when you cannot see out of it. Same shape as the
	// low-light rule: when the evidence is absent, say nothing rather than guess.
	coveredNow := verdicts[TamperCovered] || st.active[TamperCovered]
	if haveReference && !coveredNow && !vision.LowLight(fp) {
		if vision.HistogramDistanceFrom(reference, fp) >= cfg.MovedDistance {
			verdicts[TamperMoved] = true
		}
	}

	transitions := m.settle(st, verdicts, cfg)
	m.mu.Unlock()

	for kind, raised := range transitions {
		m.publish(ctx, cameraId, kind, raised)
	}
}

// settle advances the per-kind streaks and returns the transitions to announce.
// Caller holds the lock.
func (m *CameraTamperMonitor) settle(st *tamperState, verdicts map[string]bool, cfg TamperSettings) map[string]bool {
	out := map[string]bool{}
	for _, kind := range []string{TamperFrozen, TamperCovered, TamperMoved} {
		if verdicts[kind] {
			st.streak[kind]++
			if !st.active[kind] && st.streak[kind] >= cfg.FailureThreshold {
				st.active[kind] = true
				out[kind] = true
			}
			continue
		}
		st.streak[kind] = 0
		if st.active[kind] {
			st.active[kind] = false
			out[kind] = false
			if kind == TamperFrozen {
				st.frozenSince = 0
			}
		}
	}
	return out
}

func (m *CameraTamperMonitor) publish(ctx context.Context, cameraId int64, kind string, raised bool) {
	if m.notifier == nil {
		return
	}
	if raised && m.metrics != nil {
		m.metrics.Inc(MetricCameraTamperTotal, telemetry.Labels{"kind": kind})
	}
	name := fmt.Sprintf("Camera %d", cameraId)
	if m.camera != nil {
		if n := strings.TrimSpace(m.camera.DisplayName(ctx, cameraId)); n != "" {
			name = n
		}
	}

	// Written for the person reading the alert at 2am, not for the person who wrote the
	// detector: what appears to have happened, in the words they would use.
	var title, body string
	severity := notification.Critical
	if raised {
		switch kind {
		case TamperFrozen:
			title, body = "Camera picture frozen", fmt.Sprintf("%s is still connected but its picture has stopped changing", name)
		case TamperCovered:
			title, body = "Camera view blocked", fmt.Sprintf("%s can no longer see its scene — the lens may be covered, sprayed or out of focus", name)
		case TamperMoved:
			title, body = "Camera view changed", fmt.Sprintf("%s appears to be pointing somewhere else", name)
		}
	} else {
		severity = notification.Info
		title, body = "Camera view restored", fmt.Sprintf("%s is showing its normal scene again", name)
	}

	m.notifier.Publish(ctx, notification.Notification{
		Category: tamperCategory,
		Severity: severity,
		Title:    title,
		Body:     body,
		Source:   "camera-tamper-monitor",
		CameraId: cameraId,
		RefType:  "camera",
		RefId:    cameraId,
		Data: map[string]any{
			"cameraId": cameraId, "cameraName": name,
			"tamperKind": kind, "status": map[bool]string{true: "tampered", false: "clear"}[raised],
		},
	})
}

func (m *CameraTamperMonitor) currentSettings(ctx context.Context) TamperSettings {
	if m.settings == nil {
		return DefaultTamperSettings()
	}
	cfg, err := m.settings.Get(ctx)
	if err != nil {
		return DefaultTamperSettings()
	}
	return cfg
}
