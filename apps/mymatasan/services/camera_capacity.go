package services

import (
	"fmt"
	"math"
	"strings"
)

// Capacity model tunables. These are deliberately conservative, clearly-labelled
// assumptions — a spec-sheet estimate is inherently rough (±50%), so the goal is
// an honest ballpark plus a measured (live-extrapolated) number once cameras run.
const (
	// cpuUsableFraction / memUsableFraction leave headroom so the host stays
	// responsive rather than running pinned at 100%.
	cpuUsableFraction = 0.75
	memUsableFraction = 0.75

	// baseInferencesPerCoreSecAt640 is roughly how many YOLO-nano @640px inferences
	// one modern CPU core sustains per second. Throughput scales with frame area,
	// so a different frame width is corrected by (640/width)^2.
	baseInferencesPerCoreSecAt640 = 6.0
	// gpuInferenceSpeedup is a modest discrete-GPU multiplier over a single CPU
	// core for the same model. Real GPUs vary widely; this stays conservative.
	gpuInferenceSpeedup = 12.0

	// cpuDecodeStreamsPerCore is how many 1080p software-decoded live streams one
	// core sustains (~0.4 core each); hwaccel decode is far cheaper.
	cpuDecodeStreamsPerCore = 2.5
	gpuDecodeSpeedup        = 8.0

	// maxHWDecodeSessionsPerGPU caps how many concurrent hardware decode sessions one
	// GPU is assumed to sustain. Beyond raw throughput, GPUs (especially consumer
	// NVIDIA cards) limit concurrent NVDEC decode sessions at the driver level, so a
	// hardware-accelerated detection decode can hit a session ceiling before a
	// throughput ceiling. Card/driver-specific (older consumer cards capped at 3–5;
	// recent drivers lifted it substantially) — a deliberately generous middle ground;
	// verify on the host.
	maxHWDecodeSessionsPerGPU = 32

	// detectionDecodeStreamsPerCore is how many continuous detection decodes one CPU
	// core sustains. In siphon/auto modes the detector reads a persistent downscaled
	// MJPEG decode of each camera — the recorder's siphon tee when recording is on, or
	// a dedicated detect-only ffmpeg when recording is off. ffmpeg still decodes the
	// full input even at a low output fps, so the cost is close to a live decode minus
	// the display/encode path (~0.3 core each). This tee is SOFTWARE decode (no hwaccel
	// flag), so unlike live view it gets no GPU speedup. Standalone mode does one-shot
	// grabs (no persistent decode) and is excluded.
	detectionDecodeStreamsPerCore = 3.0

	// perCameraMemoryBytes is a rough per-camera working-set (decode buffers +
	// detector share).
	perCameraMemoryBytes = 250 * 1024 * 1024

	// idleCPUBaselinePercent is the assumed non-camera host load subtracted before
	// attributing CPU to cameras during live extrapolation.
	idleCPUBaselinePercent = 5.0

	// defaultRecordBitrateKbps is the assumed per-camera record bitrate (≈1080p
	// H.264) when nothing better is known.
	defaultRecordBitrateKbps = 4000

	// nvencEncodeRealtimeFactor is how much faster than realtime one NVENC session
	// encodes a recorded segment at remux (encode is offline, not playback-paced). One
	// session therefore services this many cameras' continuous recording within each
	// segment window. Conservative for modern NVIDIA encoders.
	nvencEncodeRealtimeFactor = 8.0
	// defaultNVENCSessions mirrors the recording package's default NVENC concurrency
	// when none is configured (recording.defaultNVENCConcurrency).
	defaultNVENCSessions = 2

	// minRetentionFloorDays is the least recording retention the capacity estimate
	// plans for. Disk caps the camera count at this floor rather than at the full
	// configured retention, so a small disk shortens retention instead of zeroing
	// cameras — but we still won't recommend a camera count that keeps less than
	// ~1 day of footage, a sane standard benchmark.
	minRetentionFloorDays = 1.0
)

// CameraCapacityInput is the detected/measured host state fed to the estimator.
type CameraCapacityInput struct {
	CPUCores         int
	GPUPresent       bool
	MemoryTotalBytes uint64
	// DiskFreeBytes is free space on the recording volume (0 = unknown).
	DiskFreeBytes uint64

	// Live signals for extrapolation. When ActiveCameras >= 1 and CPUPercent >= 0
	// the CPU-bound estimate is measured rather than modelled.
	CPUPercent    float64 // current host CPU utilisation (-1 = unknown)
	ActiveCameras int     // cameras currently being processed

	// MeasuredInferenceMs is the benchmarked per-inference latency of the active
	// detector (0 = not calibrated). When set and no live load is available, it
	// drives an accurate cold-start AI estimate instead of the static model.
	MeasuredInferenceMs float64

	// Workload configuration.
	AIEnabled            bool
	DetectionIntervalMs  int
	FrameWidth           int
	RecordingEnabled     bool
	RetentionDays        int
	PerCameraBitrateKbps int
	// RecordReencode is true when the at-rest storage codec re-encodes segments on
	// the host GPU (codec h264/hevc rather than copy). MaxConcurrentEncodes is the
	// configured NVENC session cap (0 = default). These add a GPU-encode ceiling.
	RecordReencode       bool
	MaxConcurrentEncodes int
	// RecordFallbackToCopy is true when the recorder stores segments as plain stream-
	// copy if the GPU re-encode can't run. With it on, a re-encode codec on a host
	// without a GPU no longer fails recording — it silently degrades to copy — so no
	// GPU-encode ceiling (and no 0-camera cap) applies.
	RecordFallbackToCopy bool
	// CaptureMode is the AI frame-source mode ("auto" | "siphon" | "standalone";
	// empty = auto). siphon/auto keep a continuous per-camera decode (counted as the
	// detection-decode workload); standalone uses cheaper one-shot grabs.
	CaptureMode string
	// DetectionDecodeHWAccel is true when the siphon tee decodes with hardware
	// acceleration (decoder.hwaccel != none). With a GPU present this lets the
	// detection-decode workload claim the GPU decode speedup instead of being CPU-bound.
	DetectionDecodeHWAccel bool
}

// CameraCapacityWorkload is one cost-centre's camera ceiling and what caps it.
type CameraCapacityWorkload struct {
	Name       string `json:"name"`  // "ai" | "recording" | "liveview" | "memory"
	Label      string `json:"label"` // human-readable
	MaxCameras int    `json:"maxCameras"`
	Limit      string `json:"limit"` // limiting resource: cpu|gpu|disk|memory
	Note       string `json:"note"`
	// Continuous workloads bound the total camera count; on-demand ones (live view)
	// are reported for context but excluded from the headline.
	Continuous bool `json:"continuous"`
}

// CameraCapacityEstimate is the full result returned to the UI.
type CameraCapacityEstimate struct {
	EstimatedMax     int                      `json:"estimatedMax"`
	CurrentCameras   int                      `json:"currentCameras"`
	LimitingWorkload string                   `json:"limitingWorkload"`
	Confidence       string                   `json:"confidence"` // "measured" | "estimated"
	Method           string                   `json:"method"`     // "live" | "static"
	Workloads        []CameraCapacityWorkload `json:"workloads"`
	Assumptions      []string                 `json:"assumptions"`

	// Recording is a rolling buffer (footage is purged after its retention), so the
	// disk never permanently fills — adding cameras shortens the retention you can
	// actually keep rather than blocking cameras. These describe that trade-off at
	// EstimatedMax cameras. AchievableRetentionDays is 0 when recording is disabled
	// or disk free space is unknown.
	ConfiguredRetentionDays int     `json:"configuredRetentionDays"`
	AchievableRetentionDays float64 `json:"achievableRetentionDays"`
	// RetentionConstrained is true when the disk can't keep the configured retention
	// at EstimatedMax cameras (so retention, not the camera count, is the trade-off).
	RetentionConstrained bool `json:"retentionConstrained"`
}

// EstimateCameraCapacity computes how many cameras the host can process, as the
// minimum across the enabled continuous workloads (AI inference, recording/disk,
// memory), plus an on-demand live-view ceiling for context. When live load is
// available it extrapolates the CPU-bound figure from real utilisation instead of
// the static model.
func EstimateCameraCapacity(in CameraCapacityInput) CameraCapacityEstimate {
	cores := in.CPUCores
	if cores < 1 {
		cores = 1
	}
	intervalMs := in.DetectionIntervalMs
	if intervalMs < 250 {
		intervalMs = defaultCaptureIntervalMs
	}
	frameWidth := in.FrameWidth
	if frameWidth < 160 {
		frameWidth = defaultCaptureFrameWidth
	}
	bitrateKbps := in.PerCameraBitrateKbps
	if bitrateKbps <= 0 {
		bitrateKbps = defaultRecordBitrateKbps
	}

	est := CameraCapacityEstimate{
		CurrentCameras: in.ActiveCameras,
		Confidence:     "estimated",
		Method:         "static",
	}
	assumptions := []string{}

	// --- CPU/GPU-bound (AI inference, optionally measured) ---
	measured := in.ActiveCameras >= 1 && in.CPUPercent >= 0
	var cpuMax int
	cpuLimit := "cpu"
	if in.GPUPresent {
		cpuLimit = "gpu"
	}
	calibrated := in.MeasuredInferenceMs > 0
	switch {
	case measured:
		// Attribute load above an idle baseline to the running cameras and project
		// the remaining headroom.
		marginal := math.Max(in.CPUPercent-idleCPUBaselinePercent, 1.0) / float64(in.ActiveCameras)
		headroom := cpuUsableFraction*100 - in.CPUPercent
		additional := 0
		if headroom > 0 {
			additional = int(math.Floor(headroom / marginal))
		}
		cpuMax = in.ActiveCameras + additional
		est.Confidence = "measured"
		est.Method = "live"
		assumptions = append(assumptions, fmt.Sprintf("CPU-bound figure measured from %d active camera(s) at %.0f%% CPU.", in.ActiveCameras, in.CPUPercent))
	case calibrated:
		// The persistent detector processes frames serially, so total throughput is
		// 1000/latency inferences/sec; per-camera demand is 1000/interval. Headroom
		// applied. GPU/CPU is already baked into the measured latency.
		throughputPerSec := 1000.0 / in.MeasuredInferenceMs
		demandPerCamera := 1000.0 / float64(intervalMs)
		cpuMax = int(math.Floor(throughputPerSec * cpuUsableFraction / demandPerCamera))
		est.Confidence = "calibrated"
		est.Method = "benchmark"
		assumptions = append(assumptions, fmt.Sprintf("AI: calibrated ~%.0fms per inference on this host%s, %dms interval.", in.MeasuredInferenceMs, gpuNote(in.GPUPresent), intervalMs))
	default:
		demandPerCamera := 1000.0 / float64(intervalMs)
		frameScale := math.Pow(640.0/float64(frameWidth), 2)
		budget := float64(cores) * cpuUsableFraction * baseInferencesPerCoreSecAt640 * frameScale
		if in.GPUPresent {
			budget *= gpuInferenceSpeedup
		}
		cpuMax = int(math.Floor(budget / demandPerCamera))
		assumptions = append(assumptions, fmt.Sprintf("AI: %d core(s)%s, ~%.0f inferences/core·s at 640px, %dms interval, %dpx frames.", cores, gpuNote(in.GPUPresent), baseInferencesPerCoreSecAt640, intervalMs, frameWidth))
	}
	cpuMax = clampNonNeg(cpuMax)

	// --- Recording / disk capacity ---
	// Recording is a rolling buffer: footage past its retention auto-purges, so the
	// disk never permanently fills. We therefore do NOT cap the camera count at the
	// *configured* retention (a small disk + long retention would wrongly read 0).
	// Instead we cap at a minimum-useful retention floor — at least ~1 day of footage
	// per camera, a sane standard benchmark — so a tight disk shortens retention down
	// to that floor rather than driving the estimate to a few minutes. The retention
	// actually achievable at the recommended count is reported below.
	retentionDays := in.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 7
	}
	floorDays := minRetentionFloorDays
	if float64(retentionDays) < floorDays {
		floorDays = float64(retentionDays)
	}
	bytesPerCameraSec := float64(bitrateKbps) * 1000 / 8
	recMax := -1
	if in.RecordingEnabled && in.DiskFreeBytes > 0 && bytesPerCameraSec > 0 {
		perCameraFloorBytes := bytesPerCameraSec * 86400 * floorDays
		recMax = int(math.Floor(float64(in.DiskFreeBytes) / perCameraFloorBytes))
		if recMax < 1 {
			// Even one camera can't hold the floor on this disk; never report 0 —
			// recommend 1 and let the retention note flag the squeeze.
			recMax = 1
		}
	}

	// --- Recording re-encode (host GPU, optional) ---
	// When the at-rest codec re-encodes (h264/hevc), each segment is encoded once on
	// the GPU at remux, gated by the NVENC semaphore. Encode is offline and faster
	// than realtime, so one session services several cameras per segment window; the
	// ceiling is sessions × realtime-factor. The NVENC encoders need an NVIDIA GPU, so
	// re-encode configured without a GPU is a misconfiguration (reported as 0).
	reencodeMax := -1
	reencodeSessions := in.MaxConcurrentEncodes
	if reencodeSessions <= 0 {
		reencodeSessions = defaultNVENCSessions
	}
	reencodeFallback := false
	if in.RecordReencode && in.RecordingEnabled {
		if in.GPUPresent {
			reencodeMax = clampNonNeg(int(math.Floor(float64(reencodeSessions) * nvencEncodeRealtimeFactor)))
		} else if in.RecordFallbackToCopy {
			// No GPU, but the recorder degrades to stream-copy instead of failing — so
			// there is no GPU-encode ceiling and recording is unaffected (just larger
			// on disk than the re-encoded size). Leave reencodeMax at -1 (no workload).
			reencodeFallback = true
		} else {
			reencodeMax = 0
		}
	}

	// --- Memory ---
	memMax := -1
	if in.MemoryTotalBytes > 0 {
		memMax = int(math.Floor(float64(in.MemoryTotalBytes) * memUsableFraction / float64(perCameraMemoryBytes)))
		memMax = clampNonNeg(memMax)
	}

	// --- Live view (on-demand concurrent decode) ---
	liveMax := int(math.Floor(float64(cores) * cpuUsableFraction * cpuDecodeStreamsPerCore))
	liveLimit := "cpu"
	if in.GPUPresent {
		liveMax = int(math.Floor(float64(liveMax) * gpuDecodeSpeedup))
		liveLimit = "gpu"
	}
	liveMax = clampNonNeg(liveMax)

	// --- Detection stream decode (siphon/auto, continuous) ---
	// siphon/auto keep ONE continuous software decode per camera (the recorder tee
	// when recording is on, or a dedicated detect-only ffmpeg when off). standalone
	// uses one-shot grabs, so it has no persistent decode. This is skipped in
	// measured-live mode because the live CPU% already includes the decode (counting
	// it as a separate ceiling would double-count). The tee is software decode, so —
	// unlike live view — no GPU speedup applies.
	captureMode := strings.ToLower(strings.TrimSpace(in.CaptureMode))
	continuousDecode := in.AIEnabled && (captureMode == "" || captureMode == "auto" || captureMode == "siphon")
	decodeMax := -1
	decodeLimit := "cpu"
	decodeHWAccel := in.DetectionDecodeHWAccel && in.GPUPresent
	if continuousDecode && !measured {
		decodeMax = int(math.Floor(float64(cores) * cpuUsableFraction * detectionDecodeStreamsPerCore))
		if decodeHWAccel {
			// Hardware-accelerated tee decode offloads to the GPU like live view does,
			// but is also bounded by the GPU's concurrent decode-session ceiling — take
			// the lower of the throughput- and session-limited figures.
			decodeMax = int(math.Floor(float64(decodeMax) * gpuDecodeSpeedup))
			if decodeMax > maxHWDecodeSessionsPerGPU {
				decodeMax = maxHWDecodeSessionsPerGPU
			}
			decodeLimit = "gpu"
		}
		decodeMax = clampNonNeg(decodeMax)
	}

	// --- Assemble workloads ---
	workloads := []CameraCapacityWorkload{}
	if in.AIEnabled {
		note := "Continuous YOLO inference across all cameras."
		if measured {
			note = "Projected from current live CPU load."
		} else if calibrated {
			note = "From a benchmarked inference on this host."
		}
		workloads = append(workloads, CameraCapacityWorkload{
			Name: "ai", Label: "AI detection", MaxCameras: cpuMax, Limit: cpuLimit, Note: note, Continuous: true,
		})
	}
	if decodeMax >= 0 {
		decodeNote := "Continuous software decode per camera in auto/siphon mode (recorder tee or detect-only stream). Standalone avoids this but samples slower (~one frame per interval)."
		decodeAssumption := fmt.Sprintf("Detection decode: ~%.1f continuous software decodes per core (CPU only — enable decoder hardware acceleration to offload it); standalone mode avoids this.", detectionDecodeStreamsPerCore)
		if decodeHWAccel {
			decodeNote = "Continuous hardware-accelerated decode per camera in auto/siphon mode (recorder tee or detect-only stream). Standalone avoids this but samples slower."
			decodeAssumption = fmt.Sprintf("Detection decode: hardware-accelerated tee, capped at ~%d concurrent GPU decode sessions (consumer GPUs limit NVDEC sessions; verify on this host). Failed hardware decodes fall back to software automatically.", maxHWDecodeSessionsPerGPU)
		}
		workloads = append(workloads, CameraCapacityWorkload{
			Name: "decode", Label: "Detection stream decode", MaxCameras: decodeMax, Limit: decodeLimit,
			Note: decodeNote, Continuous: true,
		})
		assumptions = append(assumptions, decodeAssumption)
	}
	if recMax >= 1 {
		// Caps the count at the retention floor: more cameras would keep less than
		// ~floorDays of footage. Beyond this, retention shortens (oldest auto-purges).
		workloads = append(workloads, CameraCapacityWorkload{
			Name: "recording", Label: "Recording", MaxCameras: recMax, Limit: "disk",
			Note: fmt.Sprintf("Max cameras that keep at least ~%s of footage on free disk; more would shorten retention (oldest auto-purges).", humanizeDays(floorDays)), Continuous: true,
		})
	}
	if reencodeFallback {
		assumptions = append(assumptions, "Recording re-encode is configured but no GPU is present; segments automatically store as stream-copy instead of failing — recording is unaffected, though footage is larger on disk than the re-encoded size would be.")
	}
	if reencodeMax >= 0 {
		if in.GPUPresent {
			workloads = append(workloads, CameraCapacityWorkload{
				Name: "reencode", Label: "Recording re-encode (GPU)", MaxCameras: reencodeMax, Limit: "gpu",
				Note: "Host GPU re-encodes each segment once at remux (storage codec is not Copy). Encode is offline and gated so it never blocks recording.", Continuous: true,
			})
			assumptions = append(assumptions, fmt.Sprintf("Recording re-encode: %d NVENC session(s) × ~%.0f× realtime ≈ %d cameras; encode runs once per segment at remux, queued so it never blocks recording.", reencodeSessions, nvencEncodeRealtimeFactor, reencodeMax))
		} else {
			workloads = append(workloads, CameraCapacityWorkload{
				Name: "reencode", Label: "Recording re-encode (GPU)", MaxCameras: 0, Limit: "gpu",
				Note: "Storage codec re-encodes segments but no NVIDIA GPU was detected — NVENC encode will fail. Switch the storage codec to Copy, or use camera-side H.265.", Continuous: true,
			})
			assumptions = append(assumptions, "Recording re-encode is enabled but no GPU is present; host NVENC encode will fail — use the Copy storage codec or push H.265 from the camera instead.")
		}
	}
	if memMax >= 0 {
		workloads = append(workloads, CameraCapacityWorkload{
			Name: "memory", Label: "Memory", MaxCameras: memMax, Limit: "memory",
			Note: fmt.Sprintf("~%d MB working set per camera.", perCameraMemoryBytes/(1024*1024)), Continuous: true,
		})
	}
	workloads = append(workloads, CameraCapacityWorkload{
		Name: "liveview", Label: "Live view (concurrent)", MaxCameras: liveMax, Limit: liveLimit,
		Note: "On-demand decode of simultaneously watched streams; not a total-camera limit.", Continuous: false,
	})

	// --- Headline = min across enabled continuous workloads ---
	est.EstimatedMax = -1
	for _, wl := range workloads {
		if !wl.Continuous {
			continue
		}
		if est.EstimatedMax < 0 || wl.MaxCameras < est.EstimatedMax {
			est.EstimatedMax = wl.MaxCameras
			est.LimitingWorkload = wl.Name
		}
	}
	if est.EstimatedMax < 0 {
		est.EstimatedMax = 0
	}

	// --- Recording retention achievable at the headline camera count ---
	// With EstimatedMax cameras all recording, the free disk holds this many days of
	// footage. If that's below the configured retention, the trade-off is shorter
	// retention (footage auto-purges) rather than fewer cameras.
	if in.RecordingEnabled && in.DiskFreeBytes > 0 && est.EstimatedMax > 0 && bytesPerCameraSec > 0 {
		bytesPerDayAllCameras := bytesPerCameraSec * 86400 * float64(est.EstimatedMax)
		achievable := float64(in.DiskFreeBytes) / bytesPerDayAllCameras
		est.ConfiguredRetentionDays = retentionDays
		est.AchievableRetentionDays = achievable
		if achievable < float64(retentionDays) {
			est.RetentionConstrained = true
			assumptions = append(assumptions, fmt.Sprintf("Recording: %d GB free holds ~%s at %d cameras vs the %d-day target — retention shortens (oldest footage auto-purges); lower bitrate or add storage to keep more.", in.DiskFreeBytes/(1024*1024*1024), humanizeDays(achievable), est.EstimatedMax, retentionDays))
		} else {
			assumptions = append(assumptions, fmt.Sprintf("Recording: %d GB free comfortably keeps the full %d-day retention at %d cameras.", in.DiskFreeBytes/(1024*1024*1024), retentionDays, est.EstimatedMax))
		}
	}

	if !in.AIEnabled && !in.RecordingEnabled {
		assumptions = append(assumptions, "Neither AI detection nor recording is enabled; only memory bounds the total.")
	}
	assumptions = append(assumptions, "Live view is on-demand and excluded from the total.")

	est.Workloads = workloads
	est.Assumptions = assumptions
	return est
}

// humanizeDays renders a retention span readably: hours below a day, otherwise
// days with one decimal when fractional.
func humanizeDays(days float64) string {
	if days < 1 {
		hours := days * 24
		if hours < 1 {
			return fmt.Sprintf("%.0f min", hours*60)
		}
		return fmt.Sprintf("%.1f hours", hours)
	}
	if days < 10 {
		return fmt.Sprintf("%.1f days", days)
	}
	return fmt.Sprintf("%.0f days", days)
}

func gpuNote(gpu bool) string {
	if gpu {
		return " + GPU"
	}
	return " (CPU only)"
}

func clampNonNeg(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
