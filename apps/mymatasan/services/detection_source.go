package services

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/infra/recording"
	"github.com/mysayasan/kopiv2/infra/rtsp"
	"github.com/mysayasan/kopiv2/infra/vision"
)

// DetectionSource resolves the frame the AI detector should use for a camera,
// honoring the capture mode (standalone / siphon / auto).
//
// It pins ALL modes to the recording stream when a camera has recording enabled —
// siphon reads decoded frames off the recorder, and standalone (including auto's
// fallback) grabs a one-frame JPEG from that SAME recording stream — so `auto`
// never alternates between streams and zone/line geometry stays stable. Cameras
// without a recording config fall back to the live stream (the only one available).
//
// Exposing this as a shared service means the live monitor and the rule-editor
// preview endpoint sample the exact same frame, so "what you draw on" is provably
// "what gets detected".
type DetectionSource struct {
	camera   ICameraService
	recorder *recording.Manager
	settings IRuntimeSettingsService
	client   *http.Client
}

// SiphonTeeParams derives the recorder MJPEG siphon-tee frame rate and width from
// the current capture settings. The tee runs only when the detector may read it
// (siphon or auto modes); standalone returns 0 so the recorder pays no extra
// decode. Shared by app startup and the recording-config save path.
func SiphonTeeParams(settings IRuntimeSettingsService) (fps int, width int) {
	rs, err := settings.Get(context.Background())
	if err != nil {
		return 0, 0
	}
	capCfg := rs.Vision.Capture
	mode := strings.ToLower(strings.TrimSpace(capCfg.Mode))
	if mode != "" && mode != "siphon" && mode != "auto" {
		return 0, 0
	}
	fps = capCfg.Siphon.Fps
	if fps <= 0 {
		fps = 1
	}
	return fps, capCfg.FrameWidth
}

// SiphonDecoderParams returns the hardware-decode settings the recorder/detect-only
// siphon tee should use, mirroring the runtime decoder settings so the tee decodes
// the same way the live/standalone paths do. Empty strings (incl. HWAccel "none")
// mean software decode. Shared by app startup, the recording-config save path, and
// the detection-stream resolver so all siphon decodes stay consistent.
func SiphonDecoderParams(settings IRuntimeSettingsService) (hwaccel, device, initDevice, videoDecoder string) {
	dec, err := settings.Decoder(context.Background())
	if err != nil {
		return "", "", "", ""
	}
	return dec.FFmpeg.HWAccel, dec.FFmpeg.HWAccelDevice, dec.FFmpeg.InitHWDevice, dec.FFmpeg.VideoDecoder
}

// NewDetectionSource builds the resolver. recorder may be nil (siphon unavailable).
func NewDetectionSource(camera ICameraService, recorder *recording.Manager, settings IRuntimeSettingsService, client *http.Client) *DetectionSource {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &DetectionSource{camera: camera, recorder: recorder, settings: settings, client: client}
}

// Capture returns a single JPEG frame for the camera per the active capture mode.
func (d *DetectionSource) Capture(ctx context.Context, cameraID int64) (vision.Frame, error) {
	return d.capture(ctx, cameraID, false)
}

// CaptureForLPR returns a high-resolution standalone frame suitable for license-
// plate OCR. It bypasses the low-res siphon shortcut (those frames are downscaled
// for object detection and would be unreadable) and grabs a fresh frame at the
// configured LPR width — "capture high, share down".
func (d *DetectionSource) CaptureForLPR(ctx context.Context, cameraID int64) (vision.Frame, error) {
	return d.capture(ctx, cameraID, true)
}

func (d *DetectionSource) capture(ctx context.Context, cameraID int64, highRes bool) (vision.Frame, error) {
	rs, err := d.settings.Get(ctx)
	if err != nil {
		return vision.Frame{}, err
	}
	capCfg := rs.Vision.Capture
	mode := strings.ToLower(strings.TrimSpace(capCfg.Mode))
	if mode == "" {
		mode = "auto"
	}
	frameWidth := capCfg.FrameWidth
	if frameWidth < 160 {
		frameWidth = 640
	}
	if highRes {
		// LPR needs the plate's pixels: use the dedicated (higher) LPR width and
		// skip siphon so the frame is a full-resolution standalone grab, not the
		// 640px object-detection frame.
		lprWidth := capCfg.LPR.FrameWidth
		if lprWidth < 640 {
			lprWidth = 1920
		}
		frameWidth = lprWidth
		mode = "standalone"

		// Auto-pick-highest: when ONVIF exposes a higher-resolution profile than the
		// camera's recording/live stream, capture the plate frame from THAT profile so
		// LPR isn't limited by a low-res sub-stream the user picked for recording.
		// Falls back to the normal stream resolution below when unavailable.
		lprCap := d.camera.LPRCapability(ctx, cameraID)
		if lprCap.Onvif && strings.TrimSpace(lprCap.RTSPURL) != "" {
			if decoder, derr := d.settings.Decoder(ctx); derr == nil {
				opts := MJPEGOptionsFromDecoderSettings(decoder)
				opts.MaxWidth = frameWidth
				if data, cerr := rtsp.CaptureJPEG(ctx, lprCap.RTSPURL, opts); cerr == nil {
					return vision.Frame{CameraId: cameraID, Data: data, Format: "jpeg", CapturedAt: time.Now().UTC().Unix()}, nil
				}
			}
			// On failure, fall through to the standard capture path below.
		}
	}
	staleLimit := time.Duration(capCfg.Siphon.StaleLimitMs) * time.Millisecond
	if staleLimit <= 0 {
		staleLimit = 4 * time.Second
	}

	var recURI, recFallback string
	hasRec := false
	if d.recorder != nil {
		recURI, recFallback, _, hasRec = d.recorder.RecordingSource(cameraID)
	}

	// Siphon path (siphon + auto): use a decoded frame off the recorder when one
	// is available; in auto it must also be fresh, otherwise we fall through to a
	// standalone grab of the same recording stream.
	if hasRec && (mode == "siphon" || mode == "auto") {
		if data, at, ok := d.recorder.LatestFrame(cameraID); ok {
			if mode == "siphon" || time.Since(time.Unix(at, 0)) <= staleLimit {
				return vision.Frame{CameraId: cameraID, Data: data, Format: "jpeg", CapturedAt: at}, nil
			}
		}
	}

	decoder, err := d.settings.Decoder(ctx)
	if err != nil {
		return vision.Frame{}, err
	}
	opts := MJPEGOptionsFromDecoderSettings(decoder)
	opts.MaxWidth = frameWidth

	// Standalone grab, pinned to the recording stream when the camera records.
	if hasRec {
		data, err := captureFirstJPEG(ctx, opts, recURI, recFallback)
		if err != nil {
			return vision.Frame{}, err
		}
		return vision.Frame{CameraId: cameraID, Data: data, Format: "jpeg", CapturedAt: time.Now().UTC().Unix()}, nil
	}

	// No recording config — fall back to the live stream (+ HTTP snapshot).
	src, err := d.camera.SnapshotSource(ctx, uint64(cameraID))
	if err != nil {
		return vision.Frame{}, err
	}
	if strings.TrimSpace(src.RTSPURI) != "" {
		data, rtspErr := rtsp.CaptureJPEG(ctx, src.RTSPURI, opts)
		if rtspErr == nil {
			return vision.Frame{CameraId: cameraID, Data: data, Format: "jpeg", CapturedAt: time.Now().UTC().Unix()}, nil
		}
		if strings.TrimSpace(src.URI) == "" {
			return vision.Frame{}, fmt.Errorf("rtsp capture failed and no snapshot uri available: %w", rtspErr)
		}
		data, snapErr := d.fetchSnapshot(ctx, src)
		if snapErr != nil {
			return vision.Frame{}, fmt.Errorf("rtsp capture failed (%v); snapshot fallback also failed: %w", rtspErr, snapErr)
		}
		return vision.Frame{CameraId: cameraID, Data: data, Format: "jpeg", CapturedAt: time.Now().UTC().Unix()}, nil
	}
	if strings.TrimSpace(src.URI) != "" {
		data, err := d.fetchSnapshot(ctx, src)
		if err != nil {
			return vision.Frame{}, err
		}
		return vision.Frame{CameraId: cameraID, Data: data, Format: "jpeg", CapturedAt: time.Now().UTC().Unix()}, nil
	}
	return vision.Frame{}, fmt.Errorf("camera has no snapshot or rtsp source")
}

// captureFirstJPEG grabs a single JPEG from the primary URI, falling back to the
// secondary on failure.
func captureFirstJPEG(ctx context.Context, opts rtsp.MJPEGOptions, primary, fallback string) ([]byte, error) {
	data, err := rtsp.CaptureJPEG(ctx, primary, opts)
	if err == nil {
		return data, nil
	}
	if strings.TrimSpace(fallback) != "" {
		if data2, err2 := rtsp.CaptureJPEG(ctx, fallback, opts); err2 == nil {
			return data2, nil
		}
	}
	return nil, err
}

func (d *DetectionSource) fetchSnapshot(ctx context.Context, source SnapshotSource) ([]byte, error) {
	return fetchSnapshotJPEG(ctx, d.client, source)
}
