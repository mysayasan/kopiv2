package services

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"sort"
	"time"

	"github.com/mysayasan/kopiv2/infra/vision"
)

// warmupDetector is the optional capability a detector backend implements to run a
// single inference bounded only by the context (no per-frame timeout cap), so a slow
// cold start (model load / GPU init) can complete during calibration warmup.
type warmupDetector interface {
	WarmupDetect(ctx context.Context, frame vision.Frame) ([]vision.ObjectCandidate, error)
}

// InferenceCalibration is the benchmarked per-inference latency of the active
// object detector on this host, used for accurate cold-start capacity estimates.
type InferenceCalibration struct {
	InferenceMs  float64 `json:"inferenceMs"`
	Samples      int     `json:"samples"`
	CalibratedAt int64   `json:"calibratedAt"`
}

// WarmupInference loads the detector model by running one synthetic inference,
// uncapped (bounded only by ctx) when the backend supports the warmup path. Run at
// startup so live detection never hits the per-frame timeout on a cold GPU/CUDA model
// load — which would kill and restart the worker in a loop and never warm. Returns an
// error for a nil backend (motion-only mode).
func WarmupInference(ctx context.Context, backend vision.ObjectDetector) error {
	if backend == nil {
		return errors.New("no object detector available (motion-only mode)")
	}
	frame := vision.Frame{Data: syntheticCalibrationJPEG(640, 480), Format: "jpeg", Width: 640, Height: 480}
	if w, ok := backend.(warmupDetector); ok {
		_, err := w.WarmupDetect(ctx, frame)
		return err
	}
	_, err := backend.DetectObjects(ctx, frame)
	return err
}

// CalibrateInference benchmarks the object backend by timing repeated single-frame
// inferences on a synthetic image. The first run is treated as warmup (model load
// / worker spawn) and discarded; the median of the remaining samples is returned.
// Best run while idle — concurrent live detection inflates the latency because the
// persistent worker processes frames serially.
func CalibrateInference(ctx context.Context, backend vision.ObjectDetector, samples int) (InferenceCalibration, error) {
	if backend == nil {
		return InferenceCalibration{}, errors.New("no object detector available (motion-only mode)")
	}
	if samples < 1 {
		samples = 3
	}
	frame := vision.Frame{Data: syntheticCalibrationJPEG(640, 480), Format: "jpeg", Width: 640, Height: 480}

	// Warmup: triggers model load / worker launch and is not timed.
	if err := WarmupInference(ctx, backend); err != nil {
		return InferenceCalibration{}, err
	}

	times := make([]float64, 0, samples)
	for i := 0; i < samples; i++ {
		if err := ctx.Err(); err != nil {
			return InferenceCalibration{}, err
		}
		start := time.Now()
		if _, err := backend.DetectObjects(ctx, frame); err != nil {
			return InferenceCalibration{}, err
		}
		times = append(times, float64(time.Since(start).Microseconds())/1000.0)
	}
	sort.Float64s(times)
	return InferenceCalibration{
		InferenceMs:  round1(times[len(times)/2]),
		Samples:      samples,
		CalibratedAt: time.Now().Unix(),
	}, nil
}

// syntheticCalibrationJPEG builds a non-trivial gradient image so the JPEG encoder
// and the detector do real work; content is irrelevant to timing.
func syntheticCalibrationJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: uint8((x + y) % 256), A: 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	return buf.Bytes()
}
