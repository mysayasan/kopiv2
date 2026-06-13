package services

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/rtsp"
)

func TestNormalizeRuntimeSettingsFillsDecoderDefaults(t *testing.T) {
	settings := normalizeRuntimeSettings(RuntimeSettings{})

	if settings.Decoder.MJPEG.Quality != defaultDecoderQuality {
		t.Fatalf("quality = %d, want %d", settings.Decoder.MJPEG.Quality, defaultDecoderQuality)
	}
	if settings.Decoder.MJPEG.Threads != defaultDecoderThreads {
		t.Fatalf("threads = %d, want %d", settings.Decoder.MJPEG.Threads, defaultDecoderThreads)
	}
	if settings.Decoder.FFmpeg.RTSPTransport != defaultDecoderRTSPTransport {
		t.Fatalf("rtspTransport = %q, want %q", settings.Decoder.FFmpeg.RTSPTransport, defaultDecoderRTSPTransport)
	}
	if settings.Decoder.FFmpeg.HWAccel != "none" {
		t.Fatalf("hwaccel = %q, want none", settings.Decoder.FFmpeg.HWAccel)
	}
	if settings.Decoder.FFmpeg.LowDelay == nil || !*settings.Decoder.FFmpeg.LowDelay {
		t.Fatalf("lowDelay = %v, want true", settings.Decoder.FFmpeg.LowDelay)
	}
	if settings.Decoder.FFmpeg.NoBuffer == nil || !*settings.Decoder.FFmpeg.NoBuffer {
		t.Fatalf("noBuffer = %v, want true", settings.Decoder.FFmpeg.NoBuffer)
	}
}

func TestValidateRuntimeSettingsRejectsInvalidDecoderValues(t *testing.T) {
	settings := normalizeRuntimeSettings(RuntimeSettings{})
	settings.Decoder.FFmpeg.HWAccel = "bad"
	if err := validateRuntimeSettings(settings); err == nil {
		t.Fatal("expected invalid hwaccel error")
	}

	settings = normalizeRuntimeSettings(RuntimeSettings{})
	settings.Decoder.FFmpeg.RTSPTransport = "bad"
	if err := validateRuntimeSettings(settings); err == nil {
		t.Fatal("expected invalid rtsp transport error")
	}

	settings = normalizeRuntimeSettings(RuntimeSettings{})
	settings.Decoder.FFmpeg.VideoDecoder = "h264 decoder"
	if err := validateRuntimeSettings(settings); err == nil {
		t.Fatal("expected invalid video decoder error")
	}
}

func TestMJPEGOptionsFromDecoderSettingsMapsRuntimeValues(t *testing.T) {
	lowDelay := false
	noBuffer := true
	settings := DecoderSettings{
		MJPEG: MJPEGDecoderSettings{
			FFmpegPath: "ffmpeg-custom",
			Quality:    9,
			Threads:    2,
		},
		FFmpeg: FFmpegDecoderSettings{
			RTSPTransport:   "udp",
			HWAccel:         "vaapi",
			HWAccelDevice:   "/dev/dri/renderD128",
			InitHWDevice:    "vaapi=va:/dev/dri/renderD128",
			VideoDecoder:    "h264",
			ProbeSize:       2048000,
			AnalyzeDuration: 3000000,
			LowDelay:        &lowDelay,
			NoBuffer:        &noBuffer,
		},
	}

	opts := MJPEGOptionsFromDecoderSettings(settings)
	if opts.FFmpegPath != "ffmpeg-custom" || opts.RTSPTransport != "udp" || opts.HWAccel != "vaapi" {
		t.Fatalf("basic options not mapped: %+v", opts)
	}
	if opts.LowDelay || !opts.NoBuffer || opts.Quality != 9 || opts.Threads != 2 {
		t.Fatalf("flags/options not mapped: %+v", opts)
	}
}

func TestAutoTuneRuntimeSettingsSelectsVAAPIForLinuxDevice(t *testing.T) {
	tracks, _ := json.Marshal([]rtsp.Track{{
		MediaType: "video",
		Codec:     "H265",
	}})
	current := normalizeRuntimeSettings(RuntimeSettings{})
	current.Decoder.MJPEG.FFmpegPath = "/usr/bin/ffmpeg"

	result := AutoTuneRuntimeSettings(current, []*CameraDetail{{
		Camera: entities.Camera{RTSPTracks: string(tracks)},
	}}, DecoderAutoTuneEnvironment{
		GOOS:        "linux",
		FFmpegPath:  "/usr/bin/ffmpeg",
		HWAccels:    []string{"vaapi", "vdpau"},
		VAAPIDevice: "/dev/dri/renderD128",
	})

	if result.Settings.Decoder.FFmpeg.HWAccel != "vaapi" {
		t.Fatalf("hwaccel = %q, want vaapi", result.Settings.Decoder.FFmpeg.HWAccel)
	}
	if result.Settings.Decoder.FFmpeg.HWAccelDevice != "/dev/dri/renderD128" {
		t.Fatalf("hwaccelDevice = %q", result.Settings.Decoder.FFmpeg.HWAccelDevice)
	}
	if result.Settings.Decoder.MJPEG.Quality != 8 {
		t.Fatalf("quality = %d, want 8 for H265", result.Settings.Decoder.MJPEG.Quality)
	}
	if result.Settings.Decoder.FFmpeg.ProbeSize != 2000000 {
		t.Fatalf("probeSize = %d, want 2000000", result.Settings.Decoder.FFmpeg.ProbeSize)
	}
}

func TestAutoTuneRuntimeSettingsKeepsSoftwareWhenFFmpegCapabilityCheckFails(t *testing.T) {
	current := normalizeRuntimeSettings(RuntimeSettings{})
	result := AutoTuneRuntimeSettings(current, nil, DecoderAutoTuneEnvironment{
		GOOS:        "windows",
		FFmpegError: "ffmpeg executable not found",
	})

	if result.Settings.Decoder.FFmpeg.HWAccel != "none" {
		t.Fatalf("hwaccel = %q, want none", result.Settings.Decoder.FFmpeg.HWAccel)
	}
	if !strings.Contains(strings.Join(result.Observations, "\n"), "capability check failed") {
		t.Fatalf("observations = %#v, want capability failure", result.Observations)
	}
}
