package apis

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/domain/utils/controllers"
	"github.com/mysayasan/kopiv2/infra/vision"
)

type capacityApi struct {
	camera    services.ICameraService
	settings  services.IRuntimeSettingsService
	metrics   services.IMachineMetricsProvider
	recording services.IRecordingService
	backend   vision.ObjectDetector

	mu          sync.Mutex
	calibration *services.InferenceCalibration
}

// NewCapacityApi registers the camera-capacity endpoints: GET /capacity (read-only
// estimate, any authenticated user) and POST /capacity/calibrate (admin — runs a
// detector benchmark for accurate cold-start estimates). backend may be nil in
// motion-only mode, which disables calibration.
func NewCapacityApi(router *mux.Router, camera services.ICameraService, settings services.IRuntimeSettingsService, metrics services.IMachineMetricsProvider, recording services.IRecordingService, backend vision.ObjectDetector) {
	h := &capacityApi{camera: camera, settings: settings, metrics: metrics, recording: recording, backend: backend}
	router.HandleFunc("/capacity", h.estimate).Methods("GET")
	router.HandleFunc("/capacity/calibrate", h.calibrate).Methods("POST")
}

// estimate gathers detected hardware, live host metrics, and the current camera /
// recording configuration, then returns a camera-capacity estimate (using any
// cached calibration).
func (a *capacityApi) estimate(w http.ResponseWriter, r *http.Request) {
	in, err := a.buildInput(r.Context())
	if err != nil {
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, services.EstimateCameraCapacity(in), "succeed")
}

// calibrate benchmarks the detector on this host, caches the measured latency, and
// returns a fresh estimate using it — the accurate cold-start path (no cameras
// needed). Slow on first run (model load); best run while idle.
func (a *capacityApi) calibrate(w http.ResponseWriter, r *http.Request) {
	if a.backend == nil {
		controllers.SendError(w, controllers.ErrBadRequest, "calibration needs an object detector; the detector is in motion-only mode")
		return
	}
	// Cold worker launch + model load (especially GPU/CUDA init) on the first
	// inference can far exceed the per-frame detection timeout, so give calibration a
	// generous budget rather than failing with a 500. The detector's warmup path is
	// uncapped and bounded by this context.
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	cal, err := services.CalibrateInference(ctx, a.backend, 3)
	if err != nil {
		// The worker's own error/traceback goes to stderr, not the structured log, so
		// surface the Go-side error here to make future failures diagnosable.
		log.Printf("capacity: calibration failed: %v", err)
		controllers.SendError(w, controllers.ErrInternalServerError, fmt.Sprintf("calibration failed: %v", err))
		return
	}
	a.mu.Lock()
	a.calibration = &cal
	a.mu.Unlock()

	in, err := a.buildInput(r.Context())
	if err != nil {
		// 5xx detail is hidden from clients, so log the real cause here (the estimate
		// input assembly — settings, GPU probe, metrics — not the benchmark itself).
		log.Printf("capacity: calibration succeeded but building the estimate failed: %v", err)
		controllers.SendError(w, controllers.ErrInternalServerError, err.Error())
		return
	}
	controllers.SendResult(w, services.EstimateCameraCapacity(in), "succeed")
}

// buildInput assembles the estimator input from detected hardware, live metrics,
// the camera/recording configuration, and any cached calibration.
func (a *capacityApi) buildInput(ctx context.Context) (services.CameraCapacityInput, error) {
	_, cameraCount, err := a.camera.Get(ctx, 1000, 0)
	if err != nil {
		return services.CameraCapacityInput{}, err
	}
	settings, err := a.settings.Get(ctx)
	if err != nil {
		return services.CameraCapacityInput{}, err
	}

	in := services.CameraCapacityInput{
		CPUCores:            runtime.NumCPU(),
		CPUPercent:          -1,
		ActiveCameras:       int(cameraCount),
		AIEnabled:           true,
		DetectionIntervalMs: settings.Vision.Capture.IntervalMs,
		FrameWidth:          settings.Vision.Capture.FrameWidth,
		CaptureMode:         settings.Vision.Capture.Mode,
	}

	// The siphon tee inherits the runtime decoder's hardware acceleration; reflect it
	// so the detection-decode workload claims the GPU speedup when it applies.
	if hw := strings.ToLower(strings.TrimSpace(settings.Decoder.FFmpeg.HWAccel)); hw != "" && hw != "none" {
		in.DetectionDecodeHWAccel = true
	}

	gpu := services.DetectDecoderGPUDevices(ctx)
	in.GPUPresent = len(gpu.Devices) > 0

	if a.metrics != nil {
		m := a.metrics.Sample(ctx)
		in.MemoryTotalBytes = m.MemoryTotalBytes
		in.CPUPercent = m.CPUPercent
		in.DiskFreeBytes = mostFreeDiskBytes(m.Disks)
	}

	if a.recording != nil {
		if configs, cfgErr := a.recording.ListConfigs(ctx); cfgErr == nil {
			maxRetention := 0
			for _, c := range configs {
				if c != nil && c.Enabled {
					in.RecordingEnabled = true
					if c.RetentionDays > maxRetention {
						maxRetention = c.RetentionDays
					}
				}
			}
			in.RetentionDays = maxRetention
		}
	}

	// Host-side at-rest re-encoding adds a GPU encode ceiling; reflect the configured
	// storage codec + NVENC concurrency so the estimate accounts for it.
	switch strings.ToLower(strings.TrimSpace(settings.Recording.Storage.Codec)) {
	case "h264", "hevc":
		in.RecordReencode = true
	}
	in.MaxConcurrentEncodes = settings.Recording.Storage.MaxConcurrentEncodes
	in.RecordFallbackToCopy = settings.Recording.Storage.FallbackToCopy == nil || *settings.Recording.Storage.FallbackToCopy

	a.mu.Lock()
	if a.calibration != nil {
		in.MeasuredInferenceMs = a.calibration.InferenceMs
	}
	a.mu.Unlock()

	return in, nil
}

// mostFreeDiskBytes returns the largest free space across the monitored volumes —
// a reasonable stand-in for the volume recordings land on (usually the big data
// disk).
func mostFreeDiskBytes(disks []services.MachineDiskMetric) uint64 {
	var best uint64
	for _, d := range disks {
		if d.FreeBytes > best {
			best = d.FreeBytes
		}
	}
	return best
}
