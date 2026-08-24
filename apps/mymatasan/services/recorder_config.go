package services

import (
	"context"
	"strings"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/atrest"
	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// RecorderConfigBuilder is the single thing that knows how to turn a camera's stored
// recording config into a runnable recording.RecorderConfig.
//
// It exists because that translation used to be written out by hand in THREE places —
// the startup fan-out in app wiring, the settings-save handler, and the detect-only
// stream resolver — each subtly different, and each needing to remember every field.
// That cost real bugs:
//
//   - ShredPasses was missing from the save-config site, so secure shred silently
//     degraded to a plain unlink the moment an operator saved any recording setting.
//   - The at-rest codec was captured at boot by the startup site, so changing it in
//     Settings did not apply until a restart, despite the code claiming it did.
//
// Every field added to RecorderConfig had to be remembered in three places or it was
// silently dropped in the others. Now there is one place, and the decoder/storage
// settings are read live on every build, so a Settings change applies the next time a
// recorder is (re)configured — which is what the settings UI already promises.
type RecorderConfigBuilder struct {
	camera      cameraCredentialSource
	recording   recordingConfigSource
	settings    IRuntimeSettingsService
	cipher      *atrest.Cipher
	shredPasses int
	metrics     telemetry.Metrics
	// hold is the case-file footage hold, handed to every recorder so its own retention
	// sweep — which deletes FILES by name and never reads the database — cannot shred
	// footage an open case is holding. nil = nothing is held.
	hold func(cameraId, startedAt, endedAt int64) bool
}

// The builder needs two methods, not two whole services. Declaring the interfaces here —
// at the consumer — rather than depending on ICameraService (39 methods) keeps a fake in a
// test to the two methods that matter instead of 40 stubs, and means adding a method to
// the camera service cannot break this file.
type cameraCredentialSource interface {
	SnapshotSource(ctx context.Context, id uint64) (SnapshotSource, error)
}

type recordingConfigSource interface {
	GetConfig(ctx context.Context, cameraId int64) (*entities.RecordingConfig, error)
}

// NewRecorderConfigBuilder builds the recorder-config factory. cipher may be nil
// (encryption off) and metrics may be nil (telemetry off).
func NewRecorderConfigBuilder(
	camera cameraCredentialSource,
	recordingService recordingConfigSource,
	settings IRuntimeSettingsService,
	cipher *atrest.Cipher,
	shredPasses int,
	metrics telemetry.Metrics,
) *RecorderConfigBuilder {
	return &RecorderConfigBuilder{
		camera:      camera,
		recording:   recordingService,
		settings:    settings,
		cipher:      cipher,
		shredPasses: shredPasses,
		metrics:     metrics,
	}
}

// SetFootageHold wires the case-file hold predicate. Set after construction because the
// case service is built from the recording service, which is built before this — see
// case_hold.go for the whole picture. A builder without it holds nothing, which is the
// right behaviour for an app that has no cases.
func (b *RecorderConfigBuilder) SetFootageHold(hold func(cameraId, startedAt, endedAt int64) bool) {
	b.hold = hold
}

// ErrNoStreamURL is the warning surfaced when a camera has recording enabled but no
// usable RTSP URI. It is not an error — the config is saved and the recorder simply does
// not start — but the operator has to be told, or recording silently never happens.
const ErrNoStreamURL = "camera has no RTSP URI — recording will not start until an RTSP URI is configured on the camera or a Stream URL override is set"

// decoderParams resolves the ffmpeg path and RTSP transport from the live decoder
// settings. Both are empty when settings are unavailable, which the recorder tolerates
// (it falls back to ffmpeg on PATH and TCP).
func (b *RecorderConfigBuilder) decoderParams(ctx context.Context) (ffmpegPath string, rtspTransport string) {
	if b.settings == nil {
		return "", ""
	}
	dec, err := b.settings.Decoder(ctx)
	if err != nil {
		return "", ""
	}
	return dec.MJPEG.FFmpegPath, dec.FFmpeg.RTSPTransport
}

// credentials returns the camera's stored username/password and its discovered RTSP URI.
func (b *RecorderConfigBuilder) credentials(ctx context.Context, cameraID int64) (username, password, discovered string) {
	if b.camera == nil {
		return "", "", ""
	}
	src, err := b.camera.SnapshotSource(ctx, uint64(cameraID))
	if err != nil {
		return "", "", ""
	}
	return src.Username, src.Password, src.RTSPURI
}

// ForRecording builds the NVR recorder config for one camera's stored recording config.
//
// warning is non-empty when the camera is enabled but has no usable stream; the config is
// still returned (disabled recorders are legitimate) and the caller surfaces the warning.
func (b *RecorderConfigBuilder) ForRecording(ctx context.Context, cfg *entities.RecordingConfig) (recording.RecorderConfig, string) {
	if cfg == nil {
		return recording.RecorderConfig{}, ""
	}

	username, password, discovered := b.credentials(ctx, cfg.CameraId)

	// Prefer the explicit StreamURL override; fall back to the ONVIF-discovered URI.
	// Credentials are injected into a bare override URL either way.
	rtspURI := strings.TrimSpace(cfg.StreamURL)
	if rtspURI == "" {
		rtspURI = discovered
	} else {
		rtspURI = RTSPURIWithCredentials(rtspURI, username, password)
	}
	fallbackURI := RTSPURIWithCredentials(strings.TrimSpace(cfg.FallbackStreamUrl), username, password)

	warning := ""
	if strings.TrimSpace(rtspURI) == "" && cfg.Enabled {
		warning = ErrNoStreamURL
	}

	ffmpegPath, rtspTransport := b.decoderParams(ctx)
	siphonFPS, siphonWidth := SiphonTeeParams(b.settings)
	hwAccel, hwDevice, initHWDevice, videoDecoder := SiphonDecoderParams(b.settings)

	// Read the at-rest storage codec LIVE. The startup path used to capture this once at
	// boot, which meant a codec change in Settings never applied until a restart.
	codec, quality, fallbackCopy := "", 0, true
	if b.settings != nil {
		if rec, err := b.settings.Recording(ctx); err == nil {
			codec = rec.Storage.Codec
			quality = rec.Storage.Quality
			fallbackCopy = rec.Storage.FallbackToCopy == nil || *rec.Storage.FallbackToCopy
		}
	}

	return recording.RecorderConfig{
		CameraId:           cfg.CameraId,
		Enabled:            cfg.Enabled,
		PreRollSec:         cfg.PreRollSec,
		PostRollSec:        cfg.PostRollSec,
		StoragePath:        cfg.StoragePath,
		FFmpegPath:         ffmpegPath,
		RTSPTransport:      rtspTransport,
		RTSPURI:            rtspURI,
		FallbackRTSPURI:    fallbackURI,
		SegmentMinutes:     cfg.SegmentMinutes,
		RetentionDays:      cfg.RetentionDays,
		SiphonFPS:          siphonFPS,
		SiphonWidth:        siphonWidth,
		HWAccel:            hwAccel,
		HWAccelDevice:      hwDevice,
		InitHWDevice:       initHWDevice,
		VideoDecoder:       videoDecoder,
		ShredPasses:        b.shredPasses,
		RecordCodec:        codec,
		RecordQuality:      quality,
		RecordFallbackCopy: fallbackCopy,
		Cipher:             b.cipher,
		Metrics:            b.metrics,
		RetentionHold:      b.hold,
	}, warning
}

// ForDetectOnly builds the detect-only recorder config: an AI frame source for a camera
// whose NVR recording is off. It writes no segments and keeps no clips, so it carries no
// storage/retention/cipher settings — only the stream and the siphon tee.
//
// ok is false when the camera has no usable stream, in which case no detection stream can
// be started for it.
//
// Note the stream preference differs from ForRecording: a detect-only stream may also use
// the configured LIVE-view URL, because it only needs pixels, not the recording-quality
// substream. That difference is deliberate and is the reason this is a separate method
// rather than a flag.
func (b *RecorderConfigBuilder) ForDetectOnly(ctx context.Context, cameraID int64) (recording.RecorderConfig, bool) {
	var streamURL, fallbackURL, liveURL string
	if b.recording != nil {
		if rcfg, err := b.recording.GetConfig(ctx, cameraID); err == nil && rcfg != nil {
			streamURL = strings.TrimSpace(rcfg.StreamURL)
			fallbackURL = strings.TrimSpace(rcfg.FallbackStreamUrl)
			liveURL = strings.TrimSpace(rcfg.LiveStreamUrl)
		}
	}

	username, password, discovered := b.credentials(ctx, cameraID)

	pick := streamURL
	if pick == "" {
		pick = liveURL
	}
	rtspURI := discovered
	if pick != "" {
		rtspURI = RTSPURIWithCredentials(pick, username, password)
	}
	if strings.TrimSpace(rtspURI) == "" {
		return recording.RecorderConfig{}, false
	}

	fallbackURI := ""
	if fallbackURL != "" {
		fallbackURI = RTSPURIWithCredentials(fallbackURL, username, password)
	}

	ffmpegPath, rtspTransport := b.decoderParams(ctx)
	siphonFPS, siphonWidth := SiphonTeeParams(b.settings)
	hwAccel, hwDevice, initHWDevice, videoDecoder := SiphonDecoderParams(b.settings)

	return recording.RecorderConfig{
		CameraId:        cameraID,
		Enabled:         true,
		DetectOnly:      true,
		FFmpegPath:      ffmpegPath,
		RTSPTransport:   rtspTransport,
		RTSPURI:         rtspURI,
		FallbackRTSPURI: fallbackURI,
		SiphonFPS:       siphonFPS,
		SiphonWidth:     siphonWidth,
		HWAccel:         hwAccel,
		HWAccelDevice:   hwDevice,
		InitHWDevice:    initHWDevice,
		VideoDecoder:    videoDecoder,
		Metrics:         b.metrics,
	}, true
}
