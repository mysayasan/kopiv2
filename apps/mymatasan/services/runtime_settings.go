package services

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/rtsp"
	"github.com/mysayasan/kopiv2/infra/stream"
	"github.com/mysayasan/kopiv2/infra/vision"
)

const runtimeSettingsKey = "runtime"

const (
	defaultDecoderQuality         = 7
	defaultDecoderThreads         = 1
	defaultDecoderRTSPTransport   = "tcp"
	defaultDecoderProbeSize       = 1000000
	defaultDecoderAnalyzeDuration = 1000000
)

var decoderNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

type runtimeSettingsService struct {
	repo     dbsql.IGenericRepo[entities.RuntimeSetting]
	defaults RuntimeSettings
}

// NewRuntimeSettingsService creates a runtime settings service seeded by app config defaults.
func NewRuntimeSettingsService(repo dbsql.IGenericRepo[entities.RuntimeSetting], defaults RuntimeSettings) IRuntimeSettingsService {
	return &runtimeSettingsService{repo: repo, defaults: normalizeRuntimeSettings(defaults)}
}

func (s *runtimeSettingsService) Get(ctx context.Context) (RuntimeSettings, error) {
	row, err := s.repo.GetByUnique(ctx, "", "key", runtimeSettingsKey)
	if err != nil {
		if isNoResultFoundErr(err) {
			return s.createDefaults(ctx)
		}
		return RuntimeSettings{}, err
	}

	settings := RuntimeSettings{}
	if strings.TrimSpace(row.Value) != "" {
		if err := json.Unmarshal([]byte(row.Value), &settings); err != nil {
			return RuntimeSettings{}, fmt.Errorf("parse runtime settings failed: %w", err)
		}
	}
	settings = normalizeRuntimeSettings(settings)
	return settings, nil
}

func (s *runtimeSettingsService) Save(ctx context.Context, settings RuntimeSettings) (RuntimeSettings, error) {
	settings = normalizeRuntimeSettings(settings)
	if err := validateRuntimeSettings(settings); err != nil {
		return RuntimeSettings{}, err
	}

	payload, err := json.Marshal(settings)
	if err != nil {
		return RuntimeSettings{}, err
	}
	now := time.Now().UTC().Unix()

	existing, err := s.repo.GetByUnique(ctx, "", "key", runtimeSettingsKey)
	if err == nil && existing != nil {
		existing.Value = string(payload)
		existing.UpdatedAt = now
		if _, err := s.repo.UpdateById(ctx, "", *existing); err != nil {
			return RuntimeSettings{}, err
		}
		return settings, nil
	}
	if err != nil && !isNoResultFoundErr(err) {
		return RuntimeSettings{}, err
	}

	if _, err := s.repo.Create(ctx, "", entities.RuntimeSetting{
		Key:       runtimeSettingsKey,
		Value:     string(payload),
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return RuntimeSettings{}, err
	}
	return settings, nil
}

func (s *runtimeSettingsService) Reset(ctx context.Context) (RuntimeSettings, error) {
	return s.Save(ctx, s.defaults)
}

func (s *runtimeSettingsService) Stream(ctx context.Context) (StreamSettings, error) {
	settings, err := s.Get(ctx)
	if err != nil {
		return StreamSettings{}, err
	}
	return settings.Stream, nil
}

func (s *runtimeSettingsService) Decoder(ctx context.Context) (DecoderSettings, error) {
	settings, err := s.Get(ctx)
	if err != nil {
		return DecoderSettings{}, err
	}
	return settings.Decoder, nil
}

func (s *runtimeSettingsService) createDefaults(ctx context.Context) (RuntimeSettings, error) {
	return s.Save(ctx, s.defaults)
}

func MJPEGOptionsFromDecoderSettings(settings DecoderSettings) rtsp.MJPEGOptions {
	return rtsp.MJPEGOptions{
		FFmpegPath:      settings.MJPEG.FFmpegPath,
		RTSPTransport:   settings.FFmpeg.RTSPTransport,
		HWAccel:         settings.FFmpeg.HWAccel,
		HWAccelDevice:   settings.FFmpeg.HWAccelDevice,
		InitHWDevice:    settings.FFmpeg.InitHWDevice,
		VideoDecoder:    settings.FFmpeg.VideoDecoder,
		ProbeSize:       settings.FFmpeg.ProbeSize,
		AnalyzeDuration: settings.FFmpeg.AnalyzeDuration,
		LowDelay:        boolPointerValue(settings.FFmpeg.LowDelay, true),
		NoBuffer:        boolPointerValue(settings.FFmpeg.NoBuffer, true),
		Threads:         settings.MJPEG.Threads,
		Quality:         settings.MJPEG.Quality,
	}
}

func normalizeRuntimeSettings(settings RuntimeSettings) RuntimeSettings {
	settings.Decoder.MJPEG.Quality = normalizeInt(settings.Decoder.MJPEG.Quality, defaultDecoderQuality, 2, 31)
	settings.Decoder.MJPEG.Threads = normalizeInt(settings.Decoder.MJPEG.Threads, defaultDecoderThreads, 1, 16)
	settings.Decoder.FFmpeg.RTSPTransport = strings.ToLower(strings.TrimSpace(settings.Decoder.FFmpeg.RTSPTransport))
	if settings.Decoder.FFmpeg.RTSPTransport == "" {
		settings.Decoder.FFmpeg.RTSPTransport = defaultDecoderRTSPTransport
	}
	settings.Decoder.FFmpeg.HWAccel = strings.ToLower(strings.TrimSpace(settings.Decoder.FFmpeg.HWAccel))
	if settings.Decoder.FFmpeg.HWAccel == "" {
		settings.Decoder.FFmpeg.HWAccel = "none"
	}
	settings.Decoder.FFmpeg.HWAccelDevice = strings.TrimSpace(settings.Decoder.FFmpeg.HWAccelDevice)
	settings.Decoder.FFmpeg.InitHWDevice = strings.TrimSpace(settings.Decoder.FFmpeg.InitHWDevice)
	settings.Decoder.FFmpeg.VideoDecoder = strings.TrimSpace(settings.Decoder.FFmpeg.VideoDecoder)
	settings.Decoder.FFmpeg.ProbeSize = normalizeInt(settings.Decoder.FFmpeg.ProbeSize, defaultDecoderProbeSize, 32000, 50000000)
	settings.Decoder.FFmpeg.AnalyzeDuration = normalizeInt(settings.Decoder.FFmpeg.AnalyzeDuration, defaultDecoderAnalyzeDuration, 0, 30000000)
	if settings.Decoder.FFmpeg.LowDelay == nil {
		value := true
		settings.Decoder.FFmpeg.LowDelay = &value
	}
	if settings.Decoder.FFmpeg.NoBuffer == nil {
		value := true
		settings.Decoder.FFmpeg.NoBuffer = &value
	}
	if settings.Stream.WebRTC.ICEServers == nil {
		settings.Stream.WebRTC.ICEServers = []stream.ICEServer{}
	}
	// Clamp YOLO inference params to valid ranges; zero means "use worker default".
	if settings.Vision.Yolo.Conf < 0 {
		settings.Vision.Yolo.Conf = 0
	} else if settings.Vision.Yolo.Conf > 1 {
		settings.Vision.Yolo.Conf = 1
	}
	if settings.Vision.Yolo.Iou < 0 {
		settings.Vision.Yolo.Iou = 0
	} else if settings.Vision.Yolo.Iou > 1 {
		settings.Vision.Yolo.Iou = 1
	}
	if settings.Vision.Yolo.Imgsz < 0 {
		settings.Vision.Yolo.Imgsz = 0
	}
	if settings.Vision.Yolo.MaxDet < 0 {
		settings.Vision.Yolo.MaxDet = 0
	}
	return settings
}

func boolPointerValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func validateRuntimeSettings(settings RuntimeSettings) error {
	if !validDecoderRTSPTransport(settings.Decoder.FFmpeg.RTSPTransport) {
		return fmt.Errorf("decoder.ffmpeg.rtspTransport must be one of tcp, udp, udp_multicast, http, or https")
	}
	if !validDecoderHWAccel(settings.Decoder.FFmpeg.HWAccel) {
		return fmt.Errorf("decoder.ffmpeg.hwaccel must be one of none, auto, d3d11va, dxva2, vaapi, cuda, qsv, videotoolbox, vdpau, or vulkan")
	}
	if settings.Decoder.FFmpeg.HWAccelDevice != "" && strings.ContainsAny(settings.Decoder.FFmpeg.HWAccelDevice, "\r\n") {
		return fmt.Errorf("decoder.ffmpeg.hwaccelDevice must not contain newlines")
	}
	if settings.Decoder.FFmpeg.InitHWDevice != "" && strings.ContainsAny(settings.Decoder.FFmpeg.InitHWDevice, "\r\n") {
		return fmt.Errorf("decoder.ffmpeg.initHwDevice must not contain newlines")
	}
	if settings.Decoder.FFmpeg.VideoDecoder != "" && !decoderNamePattern.MatchString(settings.Decoder.FFmpeg.VideoDecoder) {
		return fmt.Errorf("decoder.ffmpeg.videoDecoder may only contain letters, numbers, and underscores")
	}
	for idx, server := range settings.Stream.WebRTC.ICEServers {
		if len(server.URLs) == 0 {
			return fmt.Errorf("stream.webrtc.iceServers[%d].urls is required", idx)
		}
		for urlIdx, rawURL := range server.URLs {
			if strings.TrimSpace(rawURL) == "" {
				return fmt.Errorf("stream.webrtc.iceServers[%d].urls[%d] is required", idx, urlIdx)
			}
		}
	}
	return nil
}

// YoloInferenceParamsFromSettings converts the stored YOLO tuning settings into the
// vision.InferenceParams shape passed to each detection frame.
func YoloInferenceParamsFromSettings(s YoloInferenceSettings) vision.InferenceParams {
	return vision.InferenceParams{
		Conf:    s.Conf,
		Iou:     s.Iou,
		Augment: s.Augment,
		Imgsz:   s.Imgsz,
		Half:    s.Half,
		MaxDet:  s.MaxDet,
	}
}

func normalizeInt(value int, fallback int, minValue int, maxValue int) int {
	if value <= 0 {
		return fallback
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func validDecoderRTSPTransport(value string) bool {
	switch value {
	case "tcp", "udp", "udp_multicast", "http", "https":
		return true
	default:
		return false
	}
}

func validDecoderHWAccel(value string) bool {
	switch value {
	case "none", "auto", "d3d11va", "dxva2", "vaapi", "cuda", "qsv", "videotoolbox", "vdpau", "vulkan":
		return true
	default:
		return false
	}
}
