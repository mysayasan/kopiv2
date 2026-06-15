package services

import "testing"

func TestNormalizeCaptureSettingsSeedsFixedDefaults(t *testing.T) {
	c := normalizeRuntimeSettings(RuntimeSettings{}).Vision.Capture

	if c.Mode != defaultCaptureMode {
		t.Fatalf("mode = %q, want %q", c.Mode, defaultCaptureMode)
	}
	if c.IntervalMs != defaultCaptureIntervalMs {
		t.Fatalf("intervalMs = %d, want %d", c.IntervalMs, defaultCaptureIntervalMs)
	}
	if c.FrameWidth != defaultCaptureFrameWidth {
		t.Fatalf("frameWidth = %d, want %d", c.FrameWidth, defaultCaptureFrameWidth)
	}
	if c.Standalone.CaptureTimeoutMs != defaultCaptureTimeoutMs {
		t.Fatalf("captureTimeoutMs = %d, want %d", c.Standalone.CaptureTimeoutMs, defaultCaptureTimeoutMs)
	}
	if c.Siphon.Fps != defaultCaptureSiphonFps {
		t.Fatalf("siphon.fps = %d, want %d", c.Siphon.Fps, defaultCaptureSiphonFps)
	}
	if c.Siphon.StaleLimitMs != defaultCaptureSiphonStaleMs {
		t.Fatalf("siphon.staleLimitMs = %d, want %d", c.Siphon.StaleLimitMs, defaultCaptureSiphonStaleMs)
	}
}

func TestNormalizeCaptureSettingsClampsAndDefaultsMode(t *testing.T) {
	c := normalizeCaptureSettings(CaptureSettings{
		Mode:       "bogus",
		IntervalMs: 10,   // below min -> clamped to 250
		FrameWidth: 9000, // above max -> clamped to 1920
		Siphon:     CaptureSiphonSettings{Fps: 99},
	})
	if c.Mode != defaultCaptureMode {
		t.Fatalf("mode = %q, want default %q", c.Mode, defaultCaptureMode)
	}
	if c.IntervalMs != 250 {
		t.Fatalf("intervalMs = %d, want clamped 250", c.IntervalMs)
	}
	if c.FrameWidth != 1920 {
		t.Fatalf("frameWidth = %d, want clamped 1920", c.FrameWidth)
	}
	if c.Siphon.Fps != 30 {
		t.Fatalf("siphon.fps = %d, want clamped 30", c.Siphon.Fps)
	}
}

func TestValidateRuntimeSettingsRejectsBadCaptureMode(t *testing.T) {
	settings := normalizeRuntimeSettings(RuntimeSettings{})
	settings.Vision.Capture.Mode = "nope"
	if err := validateRuntimeSettings(settings); err == nil {
		t.Fatal("expected invalid capture mode error")
	}
}

func gpuEnv(deviceCount int) DecoderAutoTuneEnvironment {
	env := DecoderAutoTuneEnvironment{}
	for i := 0; i < deviceCount; i++ {
		env.GPUDevices.Devices = append(env.GPUDevices.Devices, DecoderGPUDeviceOption{Value: "0", HWAccel: "cuda"})
	}
	return env
}

func TestAutoConfigCaptureSettingsGPU(t *testing.T) {
	c := AutoConfigCaptureSettings(RuntimeSettings{}, 8, gpuEnv(1)).Vision.Capture
	if c.IntervalMs != 1000 || c.Siphon.Fps != 2 || c.FrameWidth != 640 {
		t.Fatalf("gpu config = %+v, want interval 1000/fps 2/width 640", c)
	}
	if c.Standalone.CaptureTimeoutMs != 5000 {
		t.Fatalf("captureTimeoutMs = %d, want 5000", c.Standalone.CaptureTimeoutMs)
	}
	if c.Siphon.StaleLimitMs != 2*c.IntervalMs {
		t.Fatalf("staleLimitMs = %d, want %d", c.Siphon.StaleLimitMs, 2*c.IntervalMs)
	}
}

func TestAutoConfigCaptureSettingsCPUOnly(t *testing.T) {
	c := AutoConfigCaptureSettings(RuntimeSettings{}, 2, gpuEnv(0)).Vision.Capture
	if c.IntervalMs != 2000 || c.Siphon.Fps != 1 || c.FrameWidth != 640 {
		t.Fatalf("cpu config = %+v, want interval 2000/fps 1/width 640", c)
	}
	if c.Siphon.StaleLimitMs != 4000 {
		t.Fatalf("staleLimitMs = %d, want 4000", c.Siphon.StaleLimitMs)
	}
}

func TestAutoConfigCaptureSettingsCPUManyCameras(t *testing.T) {
	c := AutoConfigCaptureSettings(RuntimeSettings{}, 4, gpuEnv(0)).Vision.Capture
	if c.IntervalMs != 3000 || c.Siphon.Fps != 1 || c.FrameWidth != 640 {
		t.Fatalf("cpu-many config = %+v, want interval 3000/fps 1/width 640", c)
	}
	if c.Siphon.StaleLimitMs != 6000 {
		t.Fatalf("staleLimitMs = %d, want 6000", c.Siphon.StaleLimitMs)
	}
}
