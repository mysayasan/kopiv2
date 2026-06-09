package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/rtsp"
	"github.com/mysayasan/kopiv2/infra/vision"
)

type VisionMonitor struct {
	onvif    IOnvifDeviceService
	vision   IVisionService
	settings IRuntimeSettingsService
	detector vision.Detector
	client   *http.Client
	interval time.Duration
	mu       sync.Mutex
	lastDiag map[string]int64
}

func NewVisionMonitor(onvif IOnvifDeviceService, visionService IVisionService, settings IRuntimeSettingsService) *VisionMonitor {
	return &VisionMonitor{
		onvif:    onvif,
		vision:   visionService,
		settings: settings,
		detector: vision.NewMotionDetector(),
		client:   &http.Client{Timeout: 8 * time.Second},
		interval: 2 * time.Second,
		lastDiag: map[string]int64{},
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
	for cameraID, cameraRules := range byCamera {
		if err := ctx.Err(); err != nil {
			return
		}
		frameCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		frame, err := m.captureFrame(frameCtx, cameraID)
		cancel()
		if err != nil {
			m.emitDiagnostics(ctx, cameraRules, "capture_failed", err.Error(), map[string]any{
				"cameraId": cameraID,
			})
			continue
		}
		detections, err := m.detector.Detect(ctx, frame, cameraRules)
		if err != nil {
			m.emitDiagnostics(ctx, cameraRules, "detect_failed", err.Error(), map[string]any{
				"cameraId":   cameraID,
				"capturedAt": frame.CapturedAt,
			})
			continue
		}
		if len(detections) == 0 {
			m.emitDiagnostics(ctx, cameraRules, "sampled", "frame captured; no detection above threshold", map[string]any{
				"cameraId":   cameraID,
				"capturedAt": frame.CapturedAt,
				"format":     frame.Format,
			})
		}
		for _, detection := range detections {
			_, _ = m.vision.CreateAlert(ctx, AlertEventRequest{
				RuleId:        detection.RuleId,
				CameraId:      detection.CameraId,
				DetectionType: detection.DetectionType,
				Label:         detection.Label,
				Confidence:    detection.Confidence,
				ZonePolygon:   detection.ZonePolygon,
				BoundingBox:   detection.BoundingBox,
				Metadata:      detection.Metadata,
			}, 0)
		}
	}
}

func (m *VisionMonitor) emitDiagnostics(ctx context.Context, rules []vision.DetectionRule, status string, message string, extra map[string]any) {
	for _, rule := range rules {
		if !m.shouldEmitDiagnostic(rule.Id, status, 30*time.Second) {
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

func (m *VisionMonitor) captureFrame(ctx context.Context, cameraID int64) (vision.Frame, error) {
	source, err := m.onvif.SnapshotSource(ctx, uint64(cameraID))
	if err != nil {
		return vision.Frame{}, err
	}
	var data []byte
	if strings.TrimSpace(source.RTSPURI) != "" {
		settings, err := m.settings.Decoder(ctx)
		if err != nil {
			return vision.Frame{}, err
		}
		data, err = rtsp.CaptureJPEG(ctx, source.RTSPURI, rtsp.MJPEGOptions{
			FFmpegPath:    settings.MJPEG.FFmpegPath,
			MaxWidth:      640,
			RTSPTransport: "tcp",
		})
		if err != nil {
			return vision.Frame{}, err
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
	return io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
}
