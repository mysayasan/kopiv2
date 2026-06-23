package services

import "testing"

func findWorkload(est CameraCapacityEstimate, name string) (CameraCapacityWorkload, bool) {
	for _, wl := range est.Workloads {
		if wl.Name == name {
			return wl, true
		}
	}
	return CameraCapacityWorkload{}, false
}

func TestEstimateCameraCapacityGPUBeatsCPU(t *testing.T) {
	base := CameraCapacityInput{
		CPUCores: 8, MemoryTotalBytes: 32 * 1024 * 1024 * 1024,
		CPUPercent: -1, AIEnabled: true, DetectionIntervalMs: 2000, FrameWidth: 640,
	}
	cpu := EstimateCameraCapacity(base)
	gpuInput := base
	gpuInput.GPUPresent = true
	gpu := EstimateCameraCapacity(gpuInput)

	cpuAI, _ := findWorkload(cpu, "ai")
	gpuAI, _ := findWorkload(gpu, "ai")
	if gpuAI.MaxCameras <= cpuAI.MaxCameras {
		t.Fatalf("GPU AI capacity (%d) should exceed CPU (%d)", gpuAI.MaxCameras, cpuAI.MaxCameras)
	}
	if gpuAI.Limit != "gpu" || cpuAI.Limit != "cpu" {
		t.Fatalf("limits: gpu=%q cpu=%q", gpuAI.Limit, cpuAI.Limit)
	}
}

func TestEstimateCameraCapacityStaticIsEstimated(t *testing.T) {
	est := EstimateCameraCapacity(CameraCapacityInput{
		CPUCores: 4, MemoryTotalBytes: 16 * 1024 * 1024 * 1024,
		CPUPercent: -1, ActiveCameras: 0, AIEnabled: true, DetectionIntervalMs: 2000, FrameWidth: 640,
	})
	if est.Confidence != "estimated" || est.Method != "static" {
		t.Fatalf("static path: confidence=%q method=%q", est.Confidence, est.Method)
	}
}

func TestEstimateCameraCapacityCalibrated(t *testing.T) {
	// 100ms/inference, 2000ms interval → ~20 inf/s budget, ~15 cameras after 0.75
	// headroom. No live load, so the calibrated branch (not static) drives it.
	est := EstimateCameraCapacity(CameraCapacityInput{
		CPUCores: 8, CPUPercent: -1, ActiveCameras: 0,
		AIEnabled: true, DetectionIntervalMs: 2000, FrameWidth: 640,
		MeasuredInferenceMs: 100,
	})
	if est.Confidence != "calibrated" || est.Method != "benchmark" {
		t.Fatalf("calibrated path: confidence=%q method=%q", est.Confidence, est.Method)
	}
	ai, _ := findWorkload(est, "ai")
	if ai.MaxCameras != 15 {
		t.Fatalf("calibrated AI capacity = %d, want 15", ai.MaxCameras)
	}
}

func TestEstimateCameraCapacityLiveBeatsCalibration(t *testing.T) {
	// With live load available, the measured path wins over a stale calibration.
	est := EstimateCameraCapacity(CameraCapacityInput{
		CPUCores: 8, CPUPercent: 25, ActiveCameras: 2,
		AIEnabled: true, DetectionIntervalMs: 2000, FrameWidth: 640,
		MeasuredInferenceMs: 100,
	})
	if est.Confidence != "measured" {
		t.Fatalf("live load should win: confidence=%q", est.Confidence)
	}
}

func TestEstimateCameraCapacityLiveExtrapolation(t *testing.T) {
	// 2 cameras at 25% CPU → ~10%/camera above a 5% baseline → headroom to 75%.
	est := EstimateCameraCapacity(CameraCapacityInput{
		CPUCores: 8, MemoryTotalBytes: 32 * 1024 * 1024 * 1024,
		CPUPercent: 25, ActiveCameras: 2, AIEnabled: true, DetectionIntervalMs: 2000, FrameWidth: 640,
	})
	if est.Confidence != "measured" || est.Method != "live" {
		t.Fatalf("live path: confidence=%q method=%q", est.Confidence, est.Method)
	}
	ai, _ := findWorkload(est, "ai")
	if ai.MaxCameras <= est.CurrentCameras {
		t.Fatalf("measured capacity (%d) should exceed current cameras (%d)", ai.MaxCameras, est.CurrentCameras)
	}
}

func TestEstimateCameraCapacityDiskDoesNotZeroCameras(t *testing.T) {
	// Tiny disk + long retention must NOT zero the camera count: recording is a
	// rolling buffer, so it caps at 1 and the disk shortens achievable retention.
	est := EstimateCameraCapacity(CameraCapacityInput{
		CPUCores: 16, GPUPresent: true, MemoryTotalBytes: 64 * 1024 * 1024 * 1024,
		DiskFreeBytes: 20 * 1024 * 1024 * 1024, // 20 GB — under 1 day for even one camera
		CPUPercent:    -1,
		AIEnabled:     true, DetectionIntervalMs: 2000, FrameWidth: 640,
		RecordingEnabled: true, RetentionDays: 30, PerCameraBitrateKbps: 4000,
	})
	if est.EstimatedMax < 1 {
		t.Fatalf("disk space must not zero the camera count, got max=%d", est.EstimatedMax)
	}
	if !est.RetentionConstrained {
		t.Fatalf("expected RetentionConstrained=true for a tiny disk")
	}
	if est.AchievableRetentionDays <= 0 || est.AchievableRetentionDays >= float64(est.ConfiguredRetentionDays) {
		t.Fatalf("achievable retention (%.2f) should be positive and below configured (%d)", est.AchievableRetentionDays, est.ConfiguredRetentionDays)
	}
}

func TestEstimateCameraCapacityRetentionFloorCapsCameras(t *testing.T) {
	// Plenty of CPU/GPU/memory but a modest disk: the estimate must balance toward at
	// least ~1 day of footage rather than reporting a huge count with minutes of
	// retention. Recording is the limiter and achievable retention stays >= ~1 day.
	est := EstimateCameraCapacity(CameraCapacityInput{
		CPUCores: 16, GPUPresent: true, MemoryTotalBytes: 64 * 1024 * 1024 * 1024,
		DiskFreeBytes: 200 * 1024 * 1024 * 1024, // 200 GB
		CPUPercent:    -1,
		AIEnabled:     true, DetectionIntervalMs: 2000, FrameWidth: 640,
		RecordingEnabled: true, RetentionDays: 30, PerCameraBitrateKbps: 4000,
	})
	if est.LimitingWorkload != "recording" {
		t.Fatalf("recording should cap a modest disk, got %q (max=%d)", est.LimitingWorkload, est.EstimatedMax)
	}
	if est.AchievableRetentionDays < 0.95 {
		t.Fatalf("achievable retention (%.2f days) should stay at/above the ~1-day floor", est.AchievableRetentionDays)
	}
	if memWL, _ := findWorkload(est, "memory"); est.EstimatedMax >= memWL.MaxCameras {
		t.Fatalf("disk floor should cap below the memory ceiling (%d), got %d", memWL.MaxCameras, est.EstimatedMax)
	}
}

func TestEstimateCameraCapacityRetentionFitsLargeDisk(t *testing.T) {
	// Ample disk should keep the full configured retention at the headline count.
	est := EstimateCameraCapacity(CameraCapacityInput{
		CPUCores: 8, CPUPercent: -1, MemoryTotalBytes: 32 * 1024 * 1024 * 1024,
		DiskFreeBytes: 64 * 1024 * 1024 * 1024 * 1024, // 64 TB
		AIEnabled:     true, DetectionIntervalMs: 2000, FrameWidth: 640,
		RecordingEnabled: true, RetentionDays: 7, PerCameraBitrateKbps: 4000,
	})
	if est.RetentionConstrained {
		t.Fatalf("ample disk should not constrain retention")
	}
	if est.AchievableRetentionDays < float64(est.ConfiguredRetentionDays) {
		t.Fatalf("achievable retention (%.1f) should meet configured (%d) on a large disk", est.AchievableRetentionDays, est.ConfiguredRetentionDays)
	}
}

func TestEstimateCameraCapacityDetectionDecodeCounted(t *testing.T) {
	// siphon/auto keep a continuous decode per camera, so a "decode" continuous
	// workload must appear in static mode and is CPU-bound (the tee is not hwaccel).
	for _, mode := range []string{"", "auto", "siphon"} {
		est := EstimateCameraCapacity(CameraCapacityInput{
			CPUCores: 8, CPUPercent: -1, ActiveCameras: 0,
			AIEnabled: true, DetectionIntervalMs: 2000, FrameWidth: 640, CaptureMode: mode,
		})
		wl, ok := findWorkload(est, "decode")
		if !ok {
			t.Fatalf("mode %q: expected a detection-decode workload", mode)
		}
		if !wl.Continuous || wl.Limit != "cpu" {
			t.Fatalf("mode %q: decode workload continuous=%v limit=%q", mode, wl.Continuous, wl.Limit)
		}
		if wl.MaxCameras != 18 { // 8 * 0.75 * 3.0
			t.Fatalf("mode %q: decode max = %d, want 18", mode, wl.MaxCameras)
		}
	}
}

func TestEstimateCameraCapacityHWAccelDecodeRaisesAndGPUBound(t *testing.T) {
	// Hardware-accelerated tee decode on a GPU host claims the GPU speedup, so the
	// decode ceiling is far higher and limited by "gpu" rather than "cpu".
	sw := EstimateCameraCapacity(CameraCapacityInput{
		CPUCores: 8, GPUPresent: true, CPUPercent: -1, ActiveCameras: 0,
		AIEnabled: true, DetectionIntervalMs: 2000, FrameWidth: 640, CaptureMode: "auto",
	})
	hw := sw
	hwIn := CameraCapacityInput{
		CPUCores: 8, GPUPresent: true, CPUPercent: -1, ActiveCameras: 0,
		AIEnabled: true, DetectionIntervalMs: 2000, FrameWidth: 640, CaptureMode: "auto",
		DetectionDecodeHWAccel: true,
	}
	hw = EstimateCameraCapacity(hwIn)

	swWL, _ := findWorkload(sw, "decode")
	hwWL, _ := findWorkload(hw, "decode")
	if swWL.Limit != "cpu" {
		t.Fatalf("software decode should be cpu-bound, got %q", swWL.Limit)
	}
	if hwWL.Limit != "gpu" || hwWL.MaxCameras <= swWL.MaxCameras {
		t.Fatalf("hwaccel decode should be gpu-bound and higher: sw=%d/%s hw=%d/%s", swWL.MaxCameras, swWL.Limit, hwWL.MaxCameras, hwWL.Limit)
	}
}

func TestEstimateCameraCapacityHWDecodeSessionCap(t *testing.T) {
	// Enough cores that throughput would far exceed the GPU decode-session ceiling;
	// the hardware decode workload must be capped at maxHWDecodeSessionsPerGPU.
	est := EstimateCameraCapacity(CameraCapacityInput{
		CPUCores: 64, GPUPresent: true, CPUPercent: -1, ActiveCameras: 0,
		AIEnabled: true, DetectionIntervalMs: 2000, FrameWidth: 640, CaptureMode: "auto",
		DetectionDecodeHWAccel: true,
	})
	wl, _ := findWorkload(est, "decode")
	if wl.MaxCameras != maxHWDecodeSessionsPerGPU {
		t.Fatalf("hwaccel decode should cap at %d sessions, got %d", maxHWDecodeSessionsPerGPU, wl.MaxCameras)
	}
}

func TestEstimateCameraCapacityStandaloneHasNoDecodeWorkload(t *testing.T) {
	est := EstimateCameraCapacity(CameraCapacityInput{
		CPUCores: 8, CPUPercent: -1, ActiveCameras: 0,
		AIEnabled: true, DetectionIntervalMs: 2000, FrameWidth: 640, CaptureMode: "standalone",
	})
	if _, ok := findWorkload(est, "decode"); ok {
		t.Fatal("standalone mode should not add a continuous decode workload")
	}
}

func TestEstimateCameraCapacityMeasuredSkipsDecodeWorkload(t *testing.T) {
	// In measured-live mode the live CPU% already includes decode; a separate decode
	// ceiling would double-count, so it must be omitted.
	est := EstimateCameraCapacity(CameraCapacityInput{
		CPUCores: 8, CPUPercent: 25, ActiveCameras: 2,
		AIEnabled: true, DetectionIntervalMs: 2000, FrameWidth: 640, CaptureMode: "auto",
	})
	if _, ok := findWorkload(est, "decode"); ok {
		t.Fatal("measured mode should not add a separate decode workload (avoids double-count)")
	}
}

func TestEstimateCameraCapacitySmallerFrameRaisesAI(t *testing.T) {
	big := EstimateCameraCapacity(CameraCapacityInput{CPUCores: 8, CPUPercent: -1, AIEnabled: true, DetectionIntervalMs: 2000, FrameWidth: 640})
	small := EstimateCameraCapacity(CameraCapacityInput{CPUCores: 8, CPUPercent: -1, AIEnabled: true, DetectionIntervalMs: 2000, FrameWidth: 320})
	bigAI, _ := findWorkload(big, "ai")
	smallAI, _ := findWorkload(small, "ai")
	if smallAI.MaxCameras <= bigAI.MaxCameras {
		t.Fatalf("smaller frames should raise AI capacity: 320px=%d 640px=%d", smallAI.MaxCameras, bigAI.MaxCameras)
	}
}
