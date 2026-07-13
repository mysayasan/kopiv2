package recording

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

type recorder interface {
	WriteFrame(data []byte, capturedAt int64)
	TriggerEvent(alertId int64, frameCapturedAt int64)
	Close()
}

// CameraStatus reports the live state of one camera's recorder.
type CameraStatus struct {
	CameraId        int64  `json:"cameraId"`
	Mode            string `json:"mode"`
	State           string `json:"state"`         // "streaming" | "stopped" | "error"
	FFmpegRunning   bool   `json:"ffmpegRunning"` // true while the ffmpeg process is alive
	LiveFiles       int    `json:"liveFiles"`     // number of segment files currently on disk
	LiveDir         string `json:"liveDir"`       // absolute path to the live segment directory
	LastError       string `json:"lastError,omitempty"`
	ActiveStreamURL string `json:"activeStreamUrl,omitempty"` // URL currently being recorded
	UsingFallback   bool   `json:"usingFallback,omitempty"`   // true when fallback stream is active
	// Retained for backwards compatibility.
	RingBufferFrames   int `json:"ringBufferFrames"`
	RingBufferCapacity int `json:"ringBufferCapacity"`
}

type statusProvider interface {
	cameraStatus() CameraStatus
}

// siphonProvider is the optional capability a recorder implements to expose its
// latest decoded frame and the stream it records, for the AI detector.
type siphonProvider interface {
	latestFrame() ([]byte, int64, bool)
	recordingSource() (string, string, string)
}

// LatestFrame returns the most recent siphoned JPEG frame for a camera (decoded
// off the recording stream) and the unix second it was captured. ok is false when
// the camera has no running recorder or no frame yet.
func (m *Manager) LatestFrame(cameraId int64) ([]byte, int64, bool) {
	m.mu.RLock()
	rec := m.recorders[cameraId]
	m.mu.RUnlock()
	if sp, ok := rec.(siphonProvider); ok {
		return sp.latestFrame()
	}
	return nil, 0, false
}

// RecordingSource returns the recording stream URI/fallback/transport for a
// camera (so the detector can grab a standalone frame from the same stream it
// records). ok is false when the camera has no enabled recording config.
func (m *Manager) RecordingSource(cameraId int64) (uri, fallback, transport string, ok bool) {
	m.mu.RLock()
	cfg, has := m.configs[cameraId]
	m.mu.RUnlock()
	if !has || strings.TrimSpace(cfg.RTSPURI) == "" {
		return "", "", "", false
	}
	return cfg.RTSPURI, cfg.FallbackRTSPURI, cfg.RTSPTransport, true
}

// Manager holds per-camera recorders and dispatches alert events.
// It is safe for concurrent use.
type Manager struct {
	mu        sync.RWMutex
	recorders map[int64]recorder
	// configs retains the last enabled config per camera so recorders can be
	// stopped and restarted on pause/resume without losing their settings.
	configs map[int64]RecorderConfig
	// detectStreams marks which recorders are detect-only AI frame sources (started
	// via EnsureDetectionStream) rather than real NVR recorders, so they can be
	// torn down independently and never confused with a recording recorder.
	detectStreams map[int64]bool
	paused        bool
	sink          SegmentSink
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewManager(sink SegmentSink) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		recorders:     map[int64]recorder{},
		configs:       map[int64]RecorderConfig{},
		detectStreams: map[int64]bool{},
		sink:          sink,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Configure adds or replaces the recorder for one camera.
// Recording requires a non-empty RTSPURI; if absent the camera is skipped.
func (m *Manager) Configure(cfg RecorderConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if old, ok := m.recorders[cfg.CameraId]; ok {
		old.Close()
		delete(m.recorders, cfg.CameraId)
		// A real recording recorder supersedes any detect-only stream for this
		// camera — its own siphon tee serves the AI detector.
		delete(m.detectStreams, cfg.CameraId)
	}
	if !cfg.Enabled {
		delete(m.configs, cfg.CameraId)
		return nil
	}
	if strings.TrimSpace(cfg.RTSPURI) == "" {
		delete(m.configs, cfg.CameraId)
		log.Printf("recording: cam%d has no RTSP URI — NVR recording skipped", cfg.CameraId)
		return nil
	}

	// Remember the config so a paused recorder can be restarted on resume.
	m.configs[cfg.CameraId] = cfg
	if m.paused {
		// Recording is paused (e.g. by the machine-health disk guard); defer the
		// actual ffmpeg start until Resume.
		return nil
	}

	r := newRTSPRecorder(cfg, m.sink)
	if err := r.Start(m.ctx); err != nil {
		return err
	}
	m.recorders[cfg.CameraId] = r
	return nil
}

// EnsureDetectionStream starts a detect-only recorder for a camera so the AI
// detector gets a continuous siphoned frame stream even when NVR recording is
// disabled. It is a no-op when a recorder already exists for the camera (a real
// recording recorder always wins, and an existing detect-only stream is kept).
// It is idempotent and safe to call every reconcile tick.
func (m *Manager) EnsureDetectionStream(cfg RecorderConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(cfg.RTSPURI) == "" {
		return nil
	}
	if _, ok := m.recorders[cfg.CameraId]; ok {
		// Already recording or already detecting — nothing to do.
		return nil
	}
	if m.paused {
		// Disk guard is active; honour it and start nothing.
		return nil
	}

	cfg.Enabled = true
	cfg.DetectOnly = true
	r := newRTSPRecorder(cfg, m.sink)
	if err := r.Start(m.ctx); err != nil {
		return err
	}
	m.recorders[cfg.CameraId] = r
	m.detectStreams[cfg.CameraId] = true
	log.Printf("recording: cam%d detect-only stream started (AI frame source, recording off)", cfg.CameraId)
	return nil
}

// StopDetectionStream stops a camera's detect-only stream. It never touches a real
// NVR recording recorder. It is idempotent.
func (m *Manager) StopDetectionStream(cameraId int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.detectStreams[cameraId] {
		return
	}
	if r, ok := m.recorders[cameraId]; ok {
		r.Close()
		delete(m.recorders, cameraId)
	}
	delete(m.detectStreams, cameraId)
	log.Printf("recording: cam%d detect-only stream stopped", cameraId)
}

// Pause stops every running recorder's ffmpeg without forgetting its config, so
// no new NVR segments are written until Resume. Used by the machine-health disk
// guard to stop a near-full volume from filling completely (which would break all
// writes, including the database). It is idempotent.
func (m *Manager) Pause() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.paused {
		return
	}
	m.paused = true
	closeAll(m.recorders)
	for id := range m.recorders {
		delete(m.recorders, id)
		// Detect-only streams aren't config-retained (the vision monitor re-creates
		// them on its next reconcile once paused clears), so forget them here.
		delete(m.detectStreams, id)
	}
	log.Printf("recording: paused (machine-health disk guard) — %d recorder configs retained", len(m.configs))
}

// Resume restarts the recorders for all retained configs after a Pause. It is
// idempotent and a no-op when not paused.
func (m *Manager) Resume() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.paused {
		return
	}
	m.paused = false
	for id, cfg := range m.configs {
		if _, ok := m.recorders[id]; ok {
			continue
		}
		r := newRTSPRecorder(cfg, m.sink)
		if err := r.Start(m.ctx); err != nil {
			log.Printf("recording: cam%d resume failed: %v", id, err)
			continue
		}
		m.recorders[id] = r
	}
	log.Printf("recording: resumed — %d recorders restarted", len(m.recorders))
}

// IsPaused reports whether recording is currently paused.
func (m *Manager) IsPaused() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.paused
}

// WriteFrame is a no-op in NVR mode; kept so the vision monitor compile-path is unchanged.
func (m *Manager) WriteFrame(cameraId int64, data []byte, capturedAt int64) {
	if capturedAt <= 0 {
		capturedAt = time.Now().UTC().Unix()
	}
	m.mu.RLock()
	r, ok := m.recorders[cameraId]
	m.mu.RUnlock()
	if ok {
		r.WriteFrame(data, capturedAt)
	}
}

// TriggerEvent notifies the camera recorder that an alert was raised.
// frameCapturedAt is the Unix second timestamp of the frame that produced the
// alert; pass 0 to fall back to the current wall clock.
func (m *Manager) TriggerEvent(cameraId int64, alertId int64, frameCapturedAt int64) {
	m.mu.RLock()
	r, ok := m.recorders[cameraId]
	m.mu.RUnlock()
	if ok {
		r.TriggerEvent(alertId, frameCapturedAt)
	} else {
		log.Printf("recording: no recorder for cam%d alert%d — recording disabled or no RTSP URI", cameraId, alertId)
	}
}

// Statuses returns the live state of all configured recorders.
func (m *Manager) Statuses() []CameraStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]CameraStatus, 0, len(m.recorders))
	for _, r := range m.recorders {
		if sp, ok := r.(statusProvider); ok {
			out = append(out, sp.cameraStatus())
		}
	}
	return out
}

// closeAll shuts recorders down concurrently. recorder.Close waits for that camera's
// in-flight segment finalization to unwind, so closing a large fleet serially would
// add up; fanning out keeps teardown bounded by the slowest single camera.
func closeAll(recorders map[int64]recorder) {
	var wg sync.WaitGroup
	for _, r := range recorders {
		wg.Add(1)
		go func(rec recorder) {
			defer wg.Done()
			rec.Close()
		}(r)
	}
	wg.Wait()
}

// Close shuts down all recorders.
func (m *Manager) Close() {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	closeAll(m.recorders)
	for id := range m.recorders {
		delete(m.recorders, id)
	}
}
