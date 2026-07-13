// Package config is mymatasan's OWN configuration: the camera, decoder, stream, vision,
// health and recording blocks.
//
// These used to live in the shared infra/config.AppConfigModel, where they were dead
// weight for every other app — nine of ~25 blocks in the shared model were mymatasan-only
// — and the leak ran both ways: infra/apphost resolved YOLO *training directories*, which
// meant the generic application host carried hardcoded knowledge of a vision feature.
// A fourth app could not be added without dragging vision config along with it.
//
// The seam is deliberately NOT a nested "app" key in config.json. The blocks stay exactly
// where they are, at the top level, and the app decodes them from the same raw document
// the host already parsed. So no deployed config file has to change: the ownership moved,
// the file format did not.
//
// What stays shared is anything a second app already uses or obviously will — security
// (encryption-at-rest, used by myseliasan), pairing and nodeStream (the fleet),
// notification, loginSecurity, and all the infra blocks. What moved here is the camera and
// vision domain, which nothing else reads.
package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mysayasan/kopiv2/infra/apphost"
	sharedconfig "github.com/mysayasan/kopiv2/infra/config"
)

// Config is the mymatasan-owned half of config.json.
type Config struct {
	Camera struct {
		FFmpegPath string `json:"ffmpegPath"`
	} `json:"camera"`
	Decoder   DecoderConfigModel   `json:"decoder"`
	Stream    StreamConfigModel    `json:"stream"`
	Vision    VisionConfigModel    `json:"vision"`
	Health    HealthConfigModel    `json:"health"`
	Recording RecordingConfigModel `json:"recording"`
}

// Load decodes mymatasan's blocks out of the raw config document.
//
// Unlike the shared loader this used to rely on, a malformed document is an ERROR rather
// than a silent all-zero config. A typo in config.json should not boot the app on defaults
// and leave the operator wondering why nothing they configured took effect.
func Load(raw []byte) (*Config, error) {
	cfg := &Config{}
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("mymatasan config: %w", err)
	}
	return cfg, nil
}

// Normalize resolves this app's data-relative paths against the writable data dir, so a
// packaged install writes under <APP>_DATA instead of the process working directory.
//
// This is the code that used to live in infra/apphost — the generic host resolving YOLO
// training directories. It belongs to the app that owns the setting.
func (c *Config) Normalize(dataDir string) {
	// Recordings/snapshots root. Historically defaulted (in the app) to a CWD-relative
	// "recordings"; resolving it here lands it under the data dir on a packaged install,
	// while ResolveWritablePath's legacy fallback keeps existing footage in place.
	snapshotDir := strings.TrimSpace(c.Vision.SnapshotDir)
	if snapshotDir == "" {
		snapshotDir = "recordings"
	}
	c.Vision.SnapshotDir = apphost.ResolveWritablePath(dataDir, snapshotDir)
	if td := strings.TrimSpace(c.Vision.Training.DataDir); td != "" {
		c.Vision.Training.DataDir = apphost.ResolveWritablePath(dataDir, td)
	}
}

// DecoderConfigModel configures frame decoding (MJPEG snapshots and the ffmpeg reader).
type DecoderConfigModel struct {
	// BrowseRoots are extra directories the server-side file picker (used to
	// choose the ffmpeg binary in Settings → Runtime) may browse, on top of the
	// built-in defaults (app dir + bin/, user home, common install locations).
	// Use this for site-specific install paths, e.g. "/data/ffmpeg".
	BrowseRoots []string `json:"browseRoots"`
	MJPEG       struct {
		FFmpegPath string `json:"ffmpegPath"`
		Quality    int    `json:"quality"`
		Threads    int    `json:"threads"`
	} `json:"mjpeg"`
	FFmpeg struct {
		RTSPTransport   string `json:"rtspTransport"`
		HWAccel         string `json:"hwaccel"`
		HWAccelDevice   string `json:"hwaccelDevice"`
		InitHWDevice    string `json:"initHwDevice"`
		VideoDecoder    string `json:"videoDecoder"`
		ProbeSize       int    `json:"probeSize"`
		AnalyzeDuration int    `json:"analyzeDuration"`
		LowDelay        *bool  `json:"lowDelay"`
		NoBuffer        *bool  `json:"noBuffer"`
	} `json:"ffmpeg"`
}

type StreamConfigModel struct {
	WebRTC        WebRTCConfigModel        `json:"webrtc"`
	MJPEGFallback MJPEGFallbackConfigModel `json:"mjpegFallback"`
}

type WebRTCConfigModel struct {
	Enabled    *bool                               `json:"enabled"`
	ICEServers []sharedconfig.WebRTCICEServerModel `json:"iceServers"`
}

type MJPEGFallbackConfigModel struct {
	Enabled *bool `json:"enabled"`
}

// HealthConfigModel holds startup settings for the camera health monitor, which
// probes camera reachability and raises online/offline notifications.
type HealthConfigModel struct {
	Enabled *bool `json:"enabled"`
	// IntervalMs is the gap between full reachability sweeps.
	IntervalMs int `json:"intervalMs"`
	// TimeoutMs is the per-probe deadline (TCP dial and RTSP deep-check).
	TimeoutMs int `json:"timeoutMs"`
	// FailureThreshold is the consecutive failed probes before declaring offline.
	FailureThreshold int `json:"failureThreshold"`
	// RecoveryThreshold is the consecutive successful probes before declaring online.
	RecoveryThreshold int `json:"recoveryThreshold"`
}

// RecordingConfigModel holds global NVR recording options that aren't per-camera.
type RecordingConfigModel struct {
	// Shred securely overwrites recorded segments before deleting them, so footage
	// can't be trivially recovered from disk. Enabled by default (see resolution in
	// the app wiring); set Enabled to false for plain, faster deletes.
	Shred struct {
		Enabled *bool `json:"enabled"`
		Passes  int   `json:"passes"`
	} `json:"shred"`
	// Storage controls the on-disk video codec for finalized segments. Default
	// (Codec "" / "copy") stores the camera's native codec with no re-encode, so
	// existing installs are unchanged. "h264"/"hevc" re-encode each segment once at
	// remux time on the GPU to shrink it; live capture and event clips always stay
	// stream-copy.
	Storage struct {
		// Codec: "copy" (default), "h264", or "hevc".
		Codec string `json:"codec"`
		// Quality is the NVENC constant-quality (CQ) target when re-encoding
		// (lower = better/larger; ~23-28 typical). 0 = default.
		Quality int `json:"quality"`
		// MaxConcurrentEncodes caps simultaneous NVENC sessions shared by remux-time
		// re-encoding and playback transcode, matching the GPU's session limit. 0 =
		// default.
		MaxConcurrentEncodes int `json:"maxConcurrentEncodes"`
		// FallbackToCopy stores a segment as plain stream-copy when the GPU re-encode
		// can't run (no usable NVENC encoder, or a runtime encode failure) instead of
		// dropping it. Pointer so an omitted value defaults to enabled; ignored in copy
		// mode.
		FallbackToCopy *bool `json:"fallbackToCopy"`
	} `json:"storage"`
}

type VisionConfigModel struct {
	Enabled                   *bool `json:"enabled"`
	IntervalMs                int   `json:"intervalMs"`
	CaptureTimeoutMs          int   `json:"captureTimeoutMs"`
	DiagnosticCooldownSeconds int   `json:"diagnosticCooldownSeconds"`
	// PersistSampledDiagnostics writes a "sampled" diagnostic alert (frame
	// captured; nothing detected) to the alert log on the diagnostic cooldown.
	// Off by default: it is a noisy heartbeat that bloats the alert_event table,
	// and capture/detect FAILURES are still logged regardless. Turn on only to
	// confirm the monitor is alive and sampling while troubleshooting.
	PersistSampledDiagnostics bool `json:"persistSampledDiagnostics"`
	// Alert-log retention. The background purge runs every AlertPurgeIntervalHours
	// (default 6). DiagnosticRetentionDays deletes Vision-monitor diagnostics older
	// than N days (default 3 — keeps the noisy heartbeat/failure rows from piling
	// up). AlertRetentionDays deletes ALL alert events (real detections included)
	// older than N days; 0 disables it so real detections are kept indefinitely.
	DiagnosticRetentionDays int                       `json:"diagnosticRetentionDays"`
	AlertRetentionDays      int                       `json:"alertRetentionDays"`
	AlertPurgeIntervalHours int                       `json:"alertPurgeIntervalHours"`
	SnapshotDir             string                    `json:"snapshotDir"`
	Detector                VisionDetectorConfigModel `json:"detector"`
	Training                VisionTrainingConfigModel `json:"training"`
}

// VisionTrainingConfigModel configures the custom-model training subsystem
// (datasets, labeled images, trained models). DataDir is the on-disk root for
// dataset images, exported YOLO datasets, and trained model weights.
type VisionTrainingConfigModel struct {
	DataDir string `json:"dataDir"`
}

type VisionDetectorConfigModel struct {
	Mode                string              `json:"mode"`
	Command             string              `json:"command"`
	Args                []string            `json:"args"`
	TimeoutMs           int                 `json:"timeoutMs"`
	UseMotionFallback   *bool               `json:"useMotionFallback"`
	UseMotionIntrusion  *bool               `json:"useMotionIntrusion"`
	MinObjectConfidence float64             `json:"minObjectConfidence"`
	ClassMap            map[string][]string `json:"classMap"`
}
