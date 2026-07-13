package recording

import (
	"context"
	"strconv"

	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

const (
	defaultPreRollSec     = 30
	defaultPostRollSec    = 10
	defaultSegmentMinutes = 15
	defaultSiphonWidth    = 640

	// hwAccelFallbackFailures is how many consecutive quick ffmpeg failures with
	// hardware decode active trigger a per-camera fallback to software decode. The
	// usual cause is the GPU running out of concurrent NVDEC decode sessions (consumer
	// cards cap these), but it also covers driver mismatches and unsupported codecs.
	hwAccelFallbackFailures = 3
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
	// SiphonFPS, when > 0, tees a downscaled MJPEG output off the recording
	// ffmpeg at this frame rate into an in-memory latest-frame buffer, so the AI
	// detector can read decoded frames off the recorder (the recording stream)
	// without opening a second RTSP connection. 0 disables the tee.
	SiphonFPS   int
	SiphonWidth int // downscale width for siphoned frames (px); 0 = default
	// Hardware-decode settings for the siphon tee's decode (and the detect-only
	// stream). They mirror the runtime decoder settings and are applied as input
	// options BEFORE -i, so they only accelerate outputs that decode (the MJPEG tee);
	// the -c:v copy segment output never decodes and is unaffected. Empty / "none"
	// HWAccel = software decode. Only emitted when the tee is active (SiphonFPS > 0).
	HWAccel       string
	HWAccelDevice string
	InitHWDevice  string
	VideoDecoder  string
	// DetectOnly runs the recorder purely as an AI-detection frame source: it keeps
	// a persistent ffmpeg that emits ONLY the siphon MJPEG tee (no NVR segments, no
	// audio, no clips), so siphon/auto capture modes get continuous fresh frames even
	// when NVR recording is disabled. SiphonFPS<=0 is treated as the default fps when
	// DetectOnly is set, since the tee is the whole point.
	DetectOnly bool
	// ShredPasses > 0 securely overwrites segment files this many times before
	// removing them (see ShredFile); 0 = plain delete.
	ShredPasses int
	// RecordCodec selects the on-disk video codec for finalized segments:
	// "" / "copy" stores the camera's native codec (no re-encode, the historical
	// behaviour); "h264" or "hevc" re-encode each segment ONCE at remux time on the
	// GPU (NVENC) to shrink it. Live capture and event clips always stay stream-copy;
	// only the background remux re-encodes, gated by a shared NVENC semaphore so
	// recording is never blocked.
	RecordCodec string
	// RecordQuality is the NVENC constant-quality (CQ) target used when re-encoding
	// (lower = better quality / larger file; ~23-28 typical). 0 = default.
	RecordQuality int
	// RecordFallbackCopy, when true (the default), stores a segment as plain stream-
	// copy if the configured GPU re-encode can't run — either because the host has no
	// usable NVENC encoder (checked once up front) or a re-encode fails at runtime.
	// This guarantees the segment is saved rather than dropped. It only matters when
	// RecordCodec re-encodes (h264/hevc); copy mode never encodes.
	RecordFallbackCopy bool
	// Cipher (optional) encrypts finalized recording segments at rest so they can be
	// crypto-erased. nil = plaintext. The live .ts and the in-progress remux are
	// briefly plaintext until the segment is finalized.
	Cipher *atrest.Cipher
	// Metrics (optional) records recorder telemetry — ffmpeg restarts and segment
	// finalize outcomes. nil is fine; use recordMetric/observeMetric, which are nil-safe.
	Metrics telemetry.Metrics
}

// Recorder metric names. These are emitted by shared infra, so they carry the neutral
// kopiv2_ prefix rather than an app name — any app that records video reports the same
// numbers.
const (
	// MetricFFmpegRestartsTotal counts capture-ffmpeg restarts per camera. A camera
	// thrashing here is failing to hold its RTSP connection; it is the earliest signal
	// of a flapping stream, well before footage goes visibly missing.
	MetricFFmpegRestartsTotal = "kopiv2_recording_ffmpeg_restarts_total"
	// MetricSegmentFinalizeTotal counts segment finalize attempts by outcome (saved,
	// discarded, failed, unsaved, quarantined). Anything but `saved` accumulating means
	// footage is not reaching the recordings list.
	MetricSegmentFinalizeTotal = "kopiv2_recording_segment_finalize_total"
)

// DescribeMetrics registers help text for the recorder's metrics. Call once at startup.
func DescribeMetrics(m telemetry.Metrics) {
	if m == nil {
		return
	}
	m.Describe(MetricFFmpegRestartsTotal, "Capture ffmpeg restarts, per camera.")
	m.Describe(MetricSegmentFinalizeTotal, "Segment finalize attempts by outcome (saved, discarded, failed, unsaved, quarantined).")
}

// countMetric increments a recorder counter. Nil-safe: telemetry is optional, and an
// instrumentation call on a hot path must never need a guard at the call site.
func (c RecorderConfig) countMetric(name string, labels telemetry.Labels) {
	if c.Metrics == nil {
		return
	}
	if labels == nil {
		labels = telemetry.Labels{}
	}
	labels["camera"] = strconv.FormatInt(c.CameraId, 10)
	c.Metrics.Inc(name, labels)
}

// SegmentResult is produced when a video segment is written to disk.
type SegmentResult struct {
	CameraId  int64
	AlertId   int64 // 0 for continuous segments; alert ID for event clips
	FilePath  string
	StartedAt int64
	EndedAt   int64
	FileSize  int64
	// Codec is the on-disk video codec of the finalized segment (e.g. "h264",
	// "hevc"), recorded so the playback path knows whether it must transcode for the
	// browser without re-probing the (encrypted) file. Empty when unknown.
	Codec string
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
