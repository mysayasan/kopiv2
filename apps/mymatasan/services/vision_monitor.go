package services

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/rtsp"
	"github.com/mysayasan/kopiv2/infra/vision"
)

type VisionMonitor struct {
	camera      ICameraService
	vision      IVisionService
	settings    IRuntimeSettingsService
	detector    vision.Detector
	recorder    *recording.Manager
	client      *http.Client
	interval    time.Duration
	timeout     time.Duration
	diagCD      time.Duration
	snapshotDir string
	mu          sync.Mutex
	lastDiag    map[string]int64
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
	return &VisionMonitor{
		camera:      camera,
		vision:      visionService,
		settings:    settings,
		detector:    detector,
		recorder:    monitor.Recorder,
		client:      &http.Client{Timeout: 8 * time.Second},
		interval:    interval,
		timeout:     timeout,
		diagCD:      diagCooldown,
		snapshotDir: monitor.SnapshotDir,
		lastDiag:    map[string]int64{},
	}
}

func (m *VisionMonitor) Start(ctx context.Context) {
	go m.run(ctx)
}

func (m *VisionMonitor) run(ctx context.Context) {
	timer := time.NewTimer(1 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			m.tick(ctx)
			timer.Reset(m.interval)
		}
	}
}

func (m *VisionMonitor) tick(ctx context.Context) {
	rules, _, err := m.vision.GetRules(ctx, 1000, 0)
	if err != nil {
		return
	}
	byCamera := activeRulesByCamera(rules, time.Now().UTC())

	// Read YOLO inference settings once per tick and attach to every frame.
	inference := vision.InferenceParams{}
	if settings, err := m.settings.Get(ctx); err == nil {
		inference = YoloInferenceParamsFromSettings(settings.Vision.Yolo)
	}

	// Capture all cameras concurrently so a slow camera doesn't delay others.
	// Detection still serialises naturally on the motion detector's internal mutex.
	var wg sync.WaitGroup
	for cameraID, cameraRules := range byCamera {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(cameraID int64, cameraRules []vision.DetectionRule) {
			defer wg.Done()
			frameCtx, cancel := context.WithTimeout(ctx, m.timeout)
			frame, err := m.captureFrame(frameCtx, cameraID)
			cancel()
			frame.Inference = inference
			if err != nil {
				m.emitDiagnostics(ctx, cameraRules, "capture_failed", err.Error(), map[string]any{
					"cameraId": cameraID,
				})
				return
			}
			if m.recorder != nil {
				m.recorder.WriteFrame(cameraID, frame.Data, frame.CapturedAt)
			}
			detections, err := m.detector.Detect(ctx, frame, cameraRules)
			if err != nil {
				m.emitDiagnostics(ctx, cameraRules, "detect_failed", err.Error(), map[string]any{
					"cameraId":   cameraID,
					"capturedAt": frame.CapturedAt,
				})
				return
			}
			if len(detections) == 0 {
				m.emitDiagnostics(ctx, cameraRules, "sampled", "frame captured; no detection above threshold", map[string]any{
					"cameraId":   cameraID,
					"capturedAt": frame.CapturedAt,
					"format":     frame.Format,
				})
			}
			snapPath := m.saveSnapshot(cameraID, frame.Data, frame.CapturedAt)
			for _, detection := range detections {
				alert, _ := m.vision.CreateAlert(ctx, AlertEventRequest{
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
				if m.recorder != nil && alert != nil {
					m.recorder.TriggerEvent(detection.CameraId, alert.Id, detection.FrameCapturedAt)
				}
			}
		}(cameraID, cameraRules)
	}
	wg.Wait()
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

func (m *VisionMonitor) saveSnapshot(cameraID int64, data []byte, capturedAt int64) string {
	if m.snapshotDir == "" || len(data) == 0 {
		return ""
	}
	dir := filepath.Join(m.snapshotDir, fmt.Sprintf("cam%d", cameraID), "snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, fmt.Sprintf("snap_%d.jpg", capturedAt))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return ""
	}
	return path
}

func (m *VisionMonitor) captureFrame(ctx context.Context, cameraID int64) (vision.Frame, error) {
	source, err := m.camera.SnapshotSource(ctx, uint64(cameraID))
	if err != nil {
		return vision.Frame{}, err
	}
	var data []byte
	if strings.TrimSpace(source.RTSPURI) != "" {
		settings, err := m.settings.Decoder(ctx)
		if err != nil {
			return vision.Frame{}, err
		}
		mjpegOptions := MJPEGOptionsFromDecoderSettings(settings)
		mjpegOptions.MaxWidth = 640
		data, err = rtsp.CaptureJPEG(ctx, source.RTSPURI, mjpegOptions)
		if err != nil {
			// RTSP capture failed (codec incompatibility, stream unreachable, etc.).
			// Fall back to the HTTP snapshot URI so cameras that can't do RTSP
			// (e.g. MJPEG-only devices whose live view falls back to snapshot polling)
			// still get frames written to the recorder ring buffer.
			if strings.TrimSpace(source.URI) == "" {
				return vision.Frame{}, fmt.Errorf("rtsp capture failed and no snapshot uri available: %w", err)
			}
			rtspErr := err
			data, err = m.fetchSnapshot(ctx, source)
			if err != nil {
				return vision.Frame{}, fmt.Errorf("rtsp capture failed (%v); snapshot fallback also failed: %w", rtspErr, err)
			}
		}
	} else if strings.TrimSpace(source.URI) != "" {
		data, err = m.fetchSnapshot(ctx, source)
		if err != nil {
			return vision.Frame{}, err
		}
	} else {
		return vision.Frame{}, fmt.Errorf("camera has no snapshot or rtsp source")
	}
	return vision.Frame{
		CameraId:   cameraID,
		Data:       data,
		Format:     "jpeg",
		CapturedAt: time.Now().UTC().Unix(),
	}, nil
}

func (m *VisionMonitor) fetchSnapshot(ctx context.Context, source SnapshotSource) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URI, nil)
	if err != nil {
		return nil, err
	}
	if source.Username != "" || source.Password != "" {
		req.SetBasicAuth(source.Username, source.Password)
	}
	req.Header.Set("Accept", "image/jpeg,*/*")
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("snapshot returned %s", resp.Status)
	}
	// Some cameras return a live MJPEG multipart stream from their snapshot endpoint.
	// Parse and extract only the first JPEG frame so ffmpeg gets valid input.
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
