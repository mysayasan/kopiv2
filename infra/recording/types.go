package recording

import "context"

const (
	defaultPreRollSec     = 30
	defaultPostRollSec    = 10
	defaultSegmentMinutes = 15
)

// RecorderConfig configures NVR recording for one camera.
type RecorderConfig struct {
	CameraId        int64
	Enabled         bool
	StoragePath     string // base directory; live segments go under cam{id}/live/
	FFmpegPath      string
	RTSPURI         string // primary RTSP URI for recording
	FallbackRTSPURI string // tried after 2 consecutive quick failures of the primary
	RTSPTransport   string // tcp|udp (default tcp)
	PreRollSec      int    // seconds before alert to include in event clip
	PostRollSec     int    // seconds after alert to include in event clip
	SegmentMinutes  int    // minutes per live segment (default 15)
	RetentionDays   int    // days to keep live segments on disk (0 = forever)
}

// SegmentResult is produced when a video segment is written to disk.
type SegmentResult struct {
	CameraId  int64
	AlertId   int64  // 0 for continuous segments; alert ID for event clips
	FilePath  string
	StartedAt int64
	EndedAt   int64
	FileSize  int64
}

// SegmentSink is implemented by apps to persist segment metadata.
type SegmentSink interface {
	SaveSegment(ctx context.Context, seg SegmentResult) error
}

func preRoll(cfg RecorderConfig) int {
	if cfg.PreRollSec > 0 {
		return cfg.PreRollSec
	}
	return defaultPreRollSec
}

func postRoll(cfg RecorderConfig) int {
	if cfg.PostRollSec > 0 {
		return cfg.PostRollSec
	}
	return defaultPostRollSec
}

func segMinutes(cfg RecorderConfig) int {
	if cfg.SegmentMinutes > 0 {
		return cfg.SegmentMinutes
	}
	return defaultSegmentMinutes
}
