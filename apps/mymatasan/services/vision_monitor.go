package services

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/safego"
	"github.com/mysayasan/kopiv2/infra/telemetry"
	"github.com/mysayasan/kopiv2/infra/vision"
)

type VisionMonitor struct {
	camera         ICameraService
	vision         IVisionService
	settings       IRuntimeSettingsService
	detector       vision.Detector
	resolver       ClassResolver
	recorder       *recording.Manager
	notifier       INotificationPublisher
	notifDests     INotificationDestinationsProvider
	client         *http.Client
	source         *DetectionSource
	detectCfg      func(ctx context.Context, cameraID int64) (recording.RecorderConfig, bool)
	metadata       *MetadataRecorder
	snapCipher     *atrest.Cipher
	interval       time.Duration
	refresh        time.Duration
	timeout        time.Duration
	diagCD         time.Duration
	persistSampled bool
	snapshotDir    string
	mu             sync.Mutex
	lastDiag       map[string]int64
	metrics        telemetry.Metrics
}

// countFrame records one sampled frame's outcome, and observeInference records how long
// a detector pass took. Both nil-safe: tests construct the monitor without telemetry.
func (m *VisionMonitor) countFrame(cameraID int64, outcome string) {
	if m.metrics == nil {
		return
	}
	m.metrics.Inc(MetricFramesTotal, telemetry.Labels{
		"camera":  strconv.FormatInt(cameraID, 10),
		"outcome": outcome,
	})
}

func (m *VisionMonitor) observeInference(cameraID int64, d time.Duration) {
	if m.metrics == nil {
		return
	}
	m.metrics.Observe(MetricInferenceDurationMs, telemetry.Labels{
		"camera": strconv.FormatInt(cameraID, 10),
	}, float64(d.Milliseconds()))
}

func (m *VisionMonitor) countAlert(cameraID int64, kind string) {
	if m.metrics == nil {
		return
	}
	m.metrics.Inc(MetricAlertsTotal, telemetry.Labels{
		"camera": strconv.FormatInt(cameraID, 10),
		"kind":   kind,
	})
}

func NewVisionMonitor(camera ICameraService, visionService IVisionService, settings IRuntimeSettingsService, monitor VisionMonitorSettings) *VisionMonitor {
	detector := monitor.Detector
	if detector == nil {
		detector = vision.NewMotionDetector()
	}
	interval := time.Duration(monitor.Interval) * time.Millisecond
	if interval <= 0 {
		interval = 2 * time.Second
	}
	timeout := time.Duration(monitor.CaptureTimeout) * time.Millisecond
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	diagCooldown := time.Duration(monitor.DiagnosticCooldownSeconds) * time.Second
	if diagCooldown <= 0 {
		diagCooldown = 30 * time.Second
	}
	// Cameras are reconciled (rules/schedule/inference re-read, samplers
	// started/stopped) on this cadence; it is independent of the per-camera
	// sampling interval and only needs to be responsive to rule edits.
	refresh := 5 * time.Second
	if refresh > interval {
		refresh = interval
	}
	client := &http.Client{Timeout: 8 * time.Second}
	return &VisionMonitor{
		camera:         camera,
		vision:         visionService,
		settings:       settings,
		detector:       detector,
		resolver:       monitor.Resolver,
		recorder:       monitor.Recorder,
		notifier:       monitor.Notifier,
		notifDests:     monitor.NotificationDestinations,
		client:         client,
		source:         NewDetectionSource(camera, monitor.Recorder, settings, client),
		detectCfg:      monitor.DetectStreamConfig,
		metadata:       monitor.Metadata,
		snapCipher:     monitor.SnapshotCipher,
		interval:       interval,
		refresh:        refresh,
		timeout:        timeout,
		diagCD:         diagCooldown,
		persistSampled: monitor.PersistSampledDiagnostics,
		snapshotDir:    monitor.SnapshotDir,
		lastDiag:       map[string]int64{},
		metrics:        monitor.Metrics,
	}
}

func (m *VisionMonitor) Start(ctx context.Context) {
	// Supervised: if the reconcile loop dies, every camera silently stops being watched.
	safego.Supervise(ctx, "mymatasan.vision.monitor", m.run)
}

func (m *VisionMonitor) run(ctx context.Context) {
	// Each camera samples on its own goroutine/timer so a slow or failing
	// camera (e.g. an RTSP source that hits the capture timeout every frame)
	// cannot throttle the sampling cadence of the healthy ones. A reconcile
	// loop keeps the running samplers in sync with the active detection rules.
	samplers := map[int64]*cameraSampler{}
	defer func() {
		for _, s := range samplers {
			s.cancel()
		}
	}()

	timer := time.NewTimer(1 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			m.reconcileSamplers(ctx, samplers)
			timer.Reset(m.refresh)
		}
	}
}

// reconcileSamplers starts a sampler for each camera that has active rules,
// updates the rules/inference of running samplers, and stops samplers whose
// camera no longer has any active rule.
func (m *VisionMonitor) reconcileSamplers(ctx context.Context, samplers map[int64]*cameraSampler) {
	rules, _, err := m.vision.GetRules(ctx, 1000, 0)
	if err != nil {
		return
	}
	byCamera := activeRulesByCamera(rules, time.Now().UTC())

	// Read YOLO inference + capture settings once per reconcile. The capture Interval
	// (ms) is the live detector-rate throttle: it sets how often each camera is sampled
	// for detection, applied to every sampler below so a change takes effect on the next
	// reconcile without a restart.
	inference := vision.InferenceParams{}
	captureMode := "auto"
	sampleInterval := m.interval
	if settings, err := m.settings.Get(ctx); err == nil {
		inference = YoloInferenceParamsFromSettings(settings.Vision.Yolo)
		if mode := strings.ToLower(strings.TrimSpace(settings.Vision.Capture.Mode)); mode != "" {
			captureMode = mode
		}
		sampleInterval = m.sampleIntervalFromSettings(settings.Vision.Capture.IntervalMs)
	}

	// Metadata recording samples a camera even with no alert rules, so the set of
	// cameras to sample is the union of rule-bearing and metadata-enabled cameras.
	sampleIDs := make(map[int64]bool, len(byCamera))
	for cameraID := range byCamera {
		sampleIDs[cameraID] = true
	}
	if m.metadata != nil {
		for cameraID := range m.metadata.EnabledCameras() {
			sampleIDs[cameraID] = true
		}
	}

	for cameraID := range sampleIDs {
		// byCamera[cameraID] is nil for a metadata-only camera; resolveRuleClasses is
		// nil-safe and sampleCamera runs the observe-only pass when there are no rules.
		cameraRules := m.resolveRuleClasses(ctx, byCamera[cameraID])
		m.reconcileDetectionStream(cameraID, captureMode)
		if s, ok := samplers[cameraID]; ok {
			s.setState(cameraRules, inference, sampleInterval)
			continue
		}
		sctx, cancel := context.WithCancel(ctx)
		s := &cameraSampler{monitor: m, cameraID: cameraID, cancel: cancel}
		s.setState(cameraRules, inference, sampleInterval)
		samplers[cameraID] = s
		// Supervised, not merely recovered. This goroutine runs detector payloads from
		// the Python worker through the alert path, so a malformed detection (a nil box,
		// a bad label) can panic it — and it used to take the whole process with it. It
		// must also RESTART: the reconcile above only starts a sampler when the map has
		// no entry for the camera, so a dead sampler would leave a live map entry behind
		// and that camera would silently stop being watched until the next process start.
		safego.Supervise(sctx, fmt.Sprintf("mymatasan.vision.sampler.cam%d", cameraID), s.loop)
	}
	for cameraID, s := range samplers {
		if !sampleIDs[cameraID] {
			s.cancel()
			delete(samplers, cameraID)
			// Camera no longer has active rules nor metadata recording — tear down any
			// detect-only stream.
			if m.recorder != nil {
				m.recorder.StopDetectionStream(cameraID)
			}
		}
	}
}

// reconcileDetectionStream keeps a camera's detection-only frame stream in sync
// with the active capture mode. In siphon/auto modes a camera that has active rules
// but NO running NVR recorder gets a persistent detect-only stream so the AI reads
// continuous fresh frames instead of slow one-shot RTSP grabs. In standalone mode,
// or when NVR recording is running (its own siphon tee serves the detector), any
// detect-only stream is stopped.
func (m *VisionMonitor) reconcileDetectionStream(cameraID int64, captureMode string) {
	if m.recorder == nil || m.detectCfg == nil {
		return
	}
	wantStream := captureMode == "auto" || captureMode == "siphon"
	if wantStream {
		// A running NVR recorder already feeds the siphon buffer; only start a
		// detect-only stream when the camera is not being recorded.
		if _, _, _, recording := m.recorder.RecordingSource(cameraID); recording {
			wantStream = false
		}
	}
	if !wantStream {
		m.recorder.StopDetectionStream(cameraID)
		return
	}
	cfg, ok := m.detectCfg(context.Background(), cameraID)
	if !ok {
		return
	}
	cfg.CameraId = cameraID
	if err := m.recorder.EnsureDetectionStream(cfg); err != nil {
		m.recorder.StopDetectionStream(cameraID)
	}
}

// cameraSampler runs an independent capture→detect→alert loop for one camera.
type cameraSampler struct {
	monitor  *VisionMonitor
	cameraID int64
	cancel   context.CancelFunc

	mu        sync.Mutex
	rules     []vision.DetectionRule
	inference vision.InferenceParams
	interval  time.Duration
}

func (s *cameraSampler) setState(rules []vision.DetectionRule, inference vision.InferenceParams, interval time.Duration) {
	s.mu.Lock()
	s.rules = rules
	s.inference = inference
	s.interval = interval
	s.mu.Unlock()
}

func (s *cameraSampler) state() ([]vision.DetectionRule, vision.InferenceParams, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rules, s.inference, s.interval
}

func (s *cameraSampler) loop(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			rules, inference, interval := s.state()
			s.monitor.sampleCamera(ctx, s.cameraID, rules, inference)
			if interval <= 0 {
				interval = s.monitor.interval
			}
			timer.Reset(interval)
		}
	}
}

// sampleIntervalFromSettings turns the runtime "Interval (ms)" capture setting into the
// live per-camera sampling cadence (the detector-rate throttle configured in
// Settings → AI → Capture). 0 falls back to the built-in interval; anything set is
// clamped to a sane [250ms, 60s] range so a stray value can't peg or stall the loop.
func (m *VisionMonitor) sampleIntervalFromSettings(intervalMs int) time.Duration {
	if intervalMs <= 0 {
		return m.interval
	}
	d := time.Duration(intervalMs) * time.Millisecond
	if d < 250*time.Millisecond {
		d = 250 * time.Millisecond
	}
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

// sampleCamera captures one frame from a camera, runs detection against its
// rules, and raises alerts. It is the per-camera body of the sampling loop.
func (m *VisionMonitor) sampleCamera(ctx context.Context, cameraID int64, cameraRules []vision.DetectionRule, inference vision.InferenceParams) {
	// Sample when the camera has alert rules OR metadata recording is on for it.
	metaOn := m.metadata != nil && m.metadata.IsEnabled(cameraID)
	if len(cameraRules) == 0 && !metaOn {
		return
	}
	// A camera with an active LPR rule captures a dedicated high-resolution frame
	// (plates need the pixels) and asks the worker to run the OCR stage; otherwise
	// it uses the normal low-res object-detection frame and OCR never runs.
	wantLPR := rulesContainLPR(cameraRules)
	// Faces, like plates, need real pixels — a face on the low-res object frame is unrecognizable —
	// so a face rule also forces the high-resolution grab and gates the worker's face stage.
	wantFace := rulesContainFace(cameraRules)
	frameCtx, cancel := context.WithTimeout(ctx, m.timeout)
	frame, err := m.captureFrame(frameCtx, cameraID, wantLPR || wantFace)
	cancel()
	frame.Inference = inference
	frame.WantLPR = wantLPR
	frame.WantFace = wantFace
	if err != nil {
		// A camera that silently fails every capture looks identical to a quiet camera in
		// the alert log. This counter is what tells them apart.
		m.countFrame(cameraID, "capture_failed")
		m.emitDiagnostics(ctx, cameraRules, "capture_failed", err.Error(), map[string]any{
			"cameraId": cameraID,
		})
		return
	}
	if m.recorder != nil {
		m.recorder.WriteFrame(cameraID, frame.Data, frame.CapturedAt)
	}
	var detections []vision.Detection
	if len(cameraRules) > 0 {
		startedAt := time.Now()
		detections, err = m.detector.Detect(ctx, frame, cameraRules)
		// Timed on both paths: a detector that is failing slowly (a wedged worker hitting
		// the capture timeout every pass) is the case worth seeing, and timing only the
		// success path would hide it.
		m.observeInference(cameraID, time.Since(startedAt))
		if err != nil {
			m.countFrame(cameraID, "detect_failed")
			m.emitDiagnostics(ctx, cameraRules, "detect_failed", err.Error(), map[string]any{
				"cameraId":   cameraID,
				"capturedAt": frame.CapturedAt,
			})
			return
		}
	}
	m.countFrame(cameraID, "ok")
	// Metadata recording: if this frame's inference was not already shared with a rule
	// detection (a metadata-only camera, or one whose only rules are motion-based), run
	// a dedicated observe-only pass so the recorder still logs what the camera saw. The
	// Observed check keeps a frame from being inferred twice when Detect already emitted.
	if metaOn && !m.metadata.Observed(cameraID, frame.CapturedAt) {
		if oc, ok := m.detector.(vision.ObservationCapable); ok {
			if oerr := oc.ObserveOnly(ctx, frame); oerr != nil {
				m.emitDiagnostics(ctx, cameraRules, "observe_failed", oerr.Error(), map[string]any{
					"cameraId":   cameraID,
					"capturedAt": frame.CapturedAt,
				})
			}
		}
	}
	// The "sampled" heartbeat (frame captured, nothing detected) is a noisy
	// per-rule diagnostic that bloats the alert log, so it is only persisted when
	// explicitly enabled for troubleshooting. Capture/detect FAILURES above are
	// always logged so real problems still surface.
	if len(cameraRules) > 0 && len(detections) == 0 && m.persistSampled {
		m.emitDiagnostics(ctx, cameraRules, "sampled", "frame captured; no detection above threshold", map[string]any{
			"cameraId":   cameraID,
			"capturedAt": frame.CapturedAt,
			"format":     frame.Format,
		})
	}
	// Alert snapshots exist only to accompany a detection; a metadata-only camera has
	// no rules and no detections, so it writes none (avoids per-frame snapshot spam).
	snapPath := ""
	if len(cameraRules) > 0 {
		snapPath = m.saveSnapshot(cameraID, frame.Data, frame.CapturedAt)
	}
	// Resolve the camera's display name and the alert-notification field config
	// once per sample so notifications carry the real name and the user's chosen
	// fields/image instead of recomputing per detection.
	cameraName := ""
	var notifyDestinations []NotificationDestination
	if len(detections) > 0 {
		cameraName = m.cameraDisplayName(ctx, cameraID)
		if m.notifDests != nil {
			notifyDestinations = m.notifDests.Destinations(ctx)
		}
	}
	// triggeredAt collects, per rule, the latest moment it fired this sample, so the
	// cooldown can be persisted once per rule rather than once per detection.
	triggeredAt := map[int64]int64{}
	for _, detection := range detections {
		alert, err := m.vision.CreateAlert(ctx, AlertEventRequest{
			RuleId:        detection.RuleId,
			CameraId:      detection.CameraId,
			DetectionType: detection.DetectionType,
			Label:         detection.Label,
			Confidence:    detection.Confidence,
			ZonePolygon:   detection.ZonePolygon,
			BoundingBox:   detection.BoundingBox,
			SnapshotPath:  snapPath,
			Metadata:      detection.Metadata,
		}, 0)
		if err != nil {
			// A dropped alert is a detection the operator never sees. It was silently
			// discarded before; at minimum it must be visible in the log.
			log.Printf("vision: cam%d rule%d: persist alert failed: %v", detection.CameraId, detection.RuleId, err)
		}
		m.countAlert(detection.CameraId, "detection")

		at := detection.FrameCapturedAt
		if at <= 0 {
			at = time.Now().UTC().Unix()
		}
		if at > triggeredAt[detection.RuleId] {
			triggeredAt[detection.RuleId] = at
		}

		if m.recorder != nil && alert != nil {
			m.recorder.TriggerEvent(detection.CameraId, alert.Id, detection.FrameCapturedAt)
		}
		NotifyVisionAlert(ctx, m.notifier, alert, cameraName, VisionAlertOptions{
			RuleName:         ruleNameByID(cameraRules, detection.RuleId),
			RawImage:         frame.Data,
			Destinations:     notifyDestinations,
			RuleDestinations: ruleDestinationsByID(cameraRules, detection.RuleId),
			ArchiveClip:      ruleArchivesClip(cameraRules, detection.RuleId),
		})
	}

	// Persist each fired rule's trigger time so its cooldown survives a restart. In-memory
	// cooldown still governs this process; this is only what a fresh process reads back.
	for ruleID, at := range triggeredAt {
		if err := m.vision.MarkRuleTriggered(ctx, ruleID, at); err != nil {
			log.Printf("vision: rule%d: persist cooldown failed: %v", ruleID, err)
		}
	}
}

// resolveRuleClasses rewrites each rule's ruleConfig.classes from registry slugs
// (categories/groups) to concrete model labels so the app-neutral detector never
// needs to know about the class registry. Rules without a classes list (legacy
// person/vehicle/animal rules) pass through untouched and keep using the static
// classMap. Group edits in the registry take effect on the next reconcile.
func (m *VisionMonitor) resolveRuleClasses(ctx context.Context, rules []vision.DetectionRule) []vision.DetectionRule {
	if m.resolver == nil || len(rules) == 0 {
		return rules
	}
	for i := range rules {
		cfg := strings.TrimSpace(rules[i].RuleConfig)
		if cfg == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(cfg), &raw); err != nil {
			continue
		}
		slugs := toStringSlice(raw["classes"])
		if len(slugs) == 0 {
			continue
		}
		resolved := m.resolver.ResolveLabels(ctx, slugs)
		raw["classes"] = resolved
		if encoded, err := json.Marshal(raw); err == nil {
			rules[i].RuleConfig = string(encoded)
		}
	}
	return rules
}

// toStringSlice coerces a decoded JSON value into a string slice, tolerating the
// []any that encoding/json produces for arrays.
func toStringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// rulesContainLPR reports whether any rule in the set is a license-plate rule,
// used to gate the worker's OCR stage per camera.
func rulesContainLPR(rules []vision.DetectionRule) bool {
	for _, rule := range rules {
		if strings.ToLower(strings.TrimSpace(rule.DetectionType)) == vision.DetectionLicensePlate {
			return true
		}
	}
	return false
}

// rulesContainFace reports whether any rule in the set is a face-recognition rule, gating the
// worker's (expensive) face detect+embed stage per camera.
func rulesContainFace(rules []vision.DetectionRule) bool {
	for _, rule := range rules {
		if strings.ToLower(strings.TrimSpace(rule.DetectionType)) == vision.DetectionFace {
			return true
		}
	}
	return false
}

// ruleNameByID returns the name of the rule with the given id, or "" if absent.
func ruleNameByID(rules []vision.DetectionRule, id int64) string {
	for _, rule := range rules {
		if rule.Id == id {
			return rule.Name
		}
	}
	return ""
}

// ruleArchivesClip reports whether this rule asks the fleet to keep a copy of its event
// clip. A rule that has since been deleted archives nothing, which is the safe direction:
// the alternative would have the control plane chase a clip for a rule nobody can name.
func ruleArchivesClip(rules []vision.DetectionRule, id int64) bool {
	for _, rule := range rules {
		if rule.Id == id {
			return rule.ArchiveClip
		}
	}
	return false
}

// ruleDestinationsByID returns the destination ids a rule routes its alerts to,
// read from its ruleConfig.destinations. Empty/absent means "all destinations".
func ruleDestinationsByID(rules []vision.DetectionRule, id int64) []string {
	for _, rule := range rules {
		if rule.Id == id {
			return ParseRuleDestinations(rule.RuleConfig)
		}
	}
	return nil
}

// ParseRuleDestinations extracts the destinations list from a rule's ruleConfig
// JSON, or nil when absent/unparseable.
func ParseRuleDestinations(ruleConfig string) []string {
	if strings.TrimSpace(ruleConfig) == "" {
		return nil
	}
	var parsed struct {
		Destinations []string `json:"destinations"`
	}
	if err := json.Unmarshal([]byte(ruleConfig), &parsed); err != nil {
		return nil
	}
	return parsed.Destinations
}

func (m *VisionMonitor) emitDiagnostics(ctx context.Context, rules []vision.DetectionRule, status string, message string, extra map[string]any) {
	for _, rule := range rules {
		if !m.shouldEmitDiagnostic(rule.Id, status, m.diagCD) {
			continue
		}
		metadata := map[string]any{
			"source":          "vision-monitor",
			"diagnostic":      true,
			"status":          status,
			"message":         message,
			"ruleThreshold":   rule.Threshold,
			"ruleMinFrames":   rule.MinFrames,
			"ruleCooldownSec": rule.CooldownSeconds,
		}
		for key, value := range extra {
			metadata[key] = value
		}
		payload, _ := json.Marshal(metadata)
		_, _ = m.vision.CreateAlert(ctx, AlertEventRequest{
			RuleId:        rule.Id,
			CameraId:      rule.CameraId,
			DetectionType: rule.DetectionType,
			Label:         "Vision monitor diagnostic",
			Confidence:    0,
			ZonePolygon:   rule.ZonePolygon,
			Metadata:      string(payload),
		}, 0)
		// Counted apart from real detections so a diagnostic flood — which is what
		// emptied the events panel once before — is legible in a scrape.
		m.countAlert(rule.CameraId, "diagnostic")
	}
}

func (m *VisionMonitor) shouldEmitDiagnostic(ruleID int64, status string, cooldown time.Duration) bool {
	key := fmt.Sprintf("%d:%s", ruleID, status)
	now := time.Now().UTC().Unix()
	m.mu.Lock()
	defer m.mu.Unlock()
	if last := m.lastDiag[key]; last > 0 && now-last < int64(cooldown.Seconds()) {
		return false
	}
	m.lastDiag[key] = now
	return true
}

func activeRulesByCamera(rules []*entities.DetectionRule, now time.Time) map[int64][]vision.DetectionRule {
	result := map[int64][]vision.DetectionRule{}
	for _, rule := range rules {
		if rule == nil || !rule.IsEnabled || rule.CameraId <= 0 {
			continue
		}
		spec := vision.DetectionRule{
			Id:              rule.Id,
			CameraId:        rule.CameraId,
			Name:            rule.Name,
			DetectionType:   rule.DetectionType,
			ZonePolygon:     rule.ZonePolygon,
			RuleConfig:      rule.RuleConfig,
			SchedulePolicy:  rule.SchedulePolicy,
			Threshold:       rule.Threshold,
			MinFrames:       rule.MinFrames,
			CooldownSeconds: rule.CooldownSeconds,
			SoundEnabled:    rule.SoundEnabled,
			ArchiveClip:     rule.ArchiveClip,
			IsEnabled:       rule.IsEnabled,
			LastTriggeredAt: rule.LastTriggeredAt,
		}
		if active, _ := vision.RuleActiveAt(spec, now); !active {
			continue
		}
		result[rule.CameraId] = append(result[rule.CameraId], spec)
	}
	return result
}

// cameraDisplayName resolves a camera's human-readable name. The camera service
// caches and invalidates names on write, so this stays cheap even with many
// cameras and reflects renames immediately.
func (m *VisionMonitor) cameraDisplayName(ctx context.Context, cameraID int64) string {
	if m.camera == nil {
		return ""
	}
	return m.camera.DisplayName(ctx, cameraID)
}

func (m *VisionMonitor) saveSnapshot(cameraID int64, data []byte, capturedAt int64) string {
	if m.snapshotDir == "" || len(data) == 0 {
		return ""
	}
	dir := filepath.Join(m.snapshotDir, fmt.Sprintf("cam%d", cameraID), "snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	// Encrypt the snapshot at rest when a cipher is configured, so a factory reset can
	// crypto-erase it. Readers detect the header and decrypt (legacy plaintext passes
	// through). Fall back to plaintext if encryption unexpectedly fails.
	if m.snapCipher != nil {
		if enc, err := m.snapCipher.EncryptBytes(data); err == nil {
			data = enc
		}
	}
	path := filepath.Join(dir, fmt.Sprintf("snap_%d.jpg", capturedAt))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return ""
	}
	return path
}

// captureFrame resolves a detection frame for the camera via the shared
// DetectionSource, which honors the configured capture mode (standalone / siphon
// / auto) and pins all modes to the recording stream when the camera records.
func (m *VisionMonitor) captureFrame(ctx context.Context, cameraID int64, wantLPR bool) (vision.Frame, error) {
	if wantLPR {
		return m.source.CaptureForLPR(ctx, cameraID)
	}
	return m.source.Capture(ctx, cameraID)
}

func (m *VisionMonitor) fetchSnapshot(ctx context.Context, source SnapshotSource) ([]byte, error) {
	return fetchSnapshotJPEG(ctx, m.client, source)
}

// fetchSnapshotJPEG fetches a single JPEG from a camera's HTTP snapshot URI,
// extracting the first frame when the camera returns an MJPEG multipart stream.
// Shared by the live monitor and the detection-source resolver.
func fetchSnapshotJPEG(ctx context.Context, client *http.Client, source SnapshotSource) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URI, nil)
	if err != nil {
		return nil, err
	}
	if source.Username != "" || source.Password != "" {
		req.SetBasicAuth(source.Username, source.Password)
	}
	req.Header.Set("Accept", "image/jpeg,*/*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("snapshot returned %s", resp.Status)
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "multipart/") {
		return fetchMJPEGFrame(resp.Body)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
}

// fetchMJPEGFrame reads the first JPEG frame from an MJPEG multipart stream body.
// It scans part headers for Content-Length and reads exactly that many bytes.
func fetchMJPEGFrame(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(io.LimitReader(r, 4*1024*1024))
	var contentLength int64
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("mjpeg read: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of part headers
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			v := strings.TrimSpace(line[len("content-length:"):])
			contentLength, _ = strconv.ParseInt(v, 10, 64)
		}
	}
	if contentLength <= 0 || contentLength > 2*1024*1024 {
		return nil, fmt.Errorf("mjpeg: missing or oversized content-length (%d)", contentLength)
	}
	data := make([]byte, contentLength)
	if _, err := io.ReadFull(br, data); err != nil {
		return nil, fmt.Errorf("mjpeg read body: %w", err)
	}
	return data, nil
}
