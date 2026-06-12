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
	CameraId      int64  `json:"cameraId"`
	Mode          string `json:"mode"`
	State         string `json:"state"`         // "streaming" | "stopped" | "error"
	FFmpegRunning   bool   `json:"ffmpegRunning"`   // true while the ffmpeg process is alive
	LiveFiles       int    `json:"liveFiles"`       // number of segment files currently on disk
	LiveDir         string `json:"liveDir"`         // absolute path to the live segment directory
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

// Manager holds per-camera recorders and dispatches alert events.
// It is safe for concurrent use.
type Manager struct {
	mu        sync.RWMutex
	recorders map[int64]recorder
	sink      SegmentSink
	ctx       context.Context
	cancel    context.CancelFunc
}

func NewManager(sink SegmentSink) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		recorders: map[int64]recorder{},
		sink:      sink,
		ctx:       ctx,
		cancel:    cancel,
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
	}
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.RTSPURI) == "" {
		log.Printf("recording: cam%d has no RTSP URI — NVR recording skipped", cfg.CameraId)
		return nil
	}

	r := newRTSPRecorder(cfg, m.sink)
	if err := r.Start(m.ctx); err != nil {
		return err
	}
	m.recorders[cfg.CameraId] = r
	return nil
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

// Close shuts down all recorders.
func (m *Manager) Close() {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, r := range m.recorders {
		r.Close()
		delete(m.recorders, id)
	}
}
