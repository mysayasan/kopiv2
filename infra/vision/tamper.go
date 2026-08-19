package vision

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"math"
	"sort"
)

// Camera tamper analysis: is this camera still showing what it is supposed to show?
//
// A covered lens, a camera turned to face a wall, a defocused ring, or a frozen stream
// that keeps delivering the same picture all leave the camera reachable and the recorder
// writing files. Reachability monitoring reports green; continuity monitoring reports
// green, because segments are being written — they are just of a wall. This is the third
// question, and it is the one an attacker answers deliberately before doing anything else.
//
// No ML, and no dependency on the detector. These are a few hundred lines of arithmetic
// over frames the recorder already decodes for the AI siphon.
//
// Everything here is a pure function over a Fingerprint so it is testable without a
// camera. The stateful part — deciding that a reading is abnormal FOR THIS CAMERA — lives
// in the monitor, because that judgement needs history and history needs somewhere to live.

// fingerprintGrid is the side of the square luma grid every comparison runs on.
//
// Small on purpose. The question is "has the whole scene changed", not "what is in it",
// and downsampling to 32×32 makes a person walking through frame — which must NOT read as
// tampering — a small perturbation while a hand over the lens is a total one. It also
// makes each comparison a thousand floats rather than a million.
const fingerprintGrid = 32

// histogramBuckets is the luma histogram resolution used for scene-change detection.
const histogramBuckets = 16

// Fingerprint is the cheap, comparable summary of one frame.
type Fingerprint struct {
	// Luma is the fingerprintGrid² grayscale grid, 0..1, row-major.
	Luma []float64
	// MeanLuma is the average brightness, 0..1.
	MeanLuma float64
	// EdgeEnergy is the variance of a discrete Laplacian over the grid — the standard
	// cheap focus measure. High on a sharp scene, near zero on a blurred, covered or
	// blank one.
	EdgeEnergy float64
	// Histogram is the normalized luma histogram (sums to 1).
	Histogram []float64
}

// NewFingerprint decodes an image (the recorder siphons JPEG) and summarizes it.
func NewFingerprint(frame []byte) (*Fingerprint, error) {
	if len(frame) == 0 {
		return nil, fmt.Errorf("empty frame")
	}
	img, _, err := image.Decode(bytes.NewReader(frame))
	if err != nil {
		// The siphon emits JPEG; try it directly in case the generic decoder was not
		// registered for this build.
		img, err = jpeg.Decode(bytes.NewReader(frame))
		if err != nil {
			return nil, err
		}
	}
	return FingerprintImage(img), nil
}

// FingerprintImage summarizes an already-decoded image.
func FingerprintImage(img image.Image) *Fingerprint {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	fp := &Fingerprint{
		Luma:      make([]float64, fingerprintGrid*fingerprintGrid),
		Histogram: make([]float64, histogramBuckets),
	}
	if w <= 0 || h <= 0 {
		return fp
	}

	// Nearest-neighbour downsample. Box-filtering would be marginally more faithful and
	// is not worth it: every measure below is a whole-frame statistic, and the sampling
	// grid is fixed so two frames of the same scene sample the same points.
	var sum float64
	for gy := 0; gy < fingerprintGrid; gy++ {
		sy := b.Min.Y + (gy*h)/fingerprintGrid
		for gx := 0; gx < fingerprintGrid; gx++ {
			sx := b.Min.X + (gx*w)/fingerprintGrid
			r, g, bl, _ := img.At(sx, sy).RGBA()
			// Rec. 601 luma, normalized to 0..1 (RGBA returns 16-bit).
			y := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 65535.0
			if y > 1 {
				y = 1
			}
			fp.Luma[gy*fingerprintGrid+gx] = y
			sum += y
			bucket := int(y * float64(histogramBuckets))
			if bucket >= histogramBuckets {
				bucket = histogramBuckets - 1
			}
			fp.Histogram[bucket]++
		}
	}

	n := float64(len(fp.Luma))
	fp.MeanLuma = sum / n
	for i := range fp.Histogram {
		fp.Histogram[i] /= n
	}
	fp.EdgeEnergy = laplacianVariance(fp.Luma, fingerprintGrid)
	return fp
}

// laplacianVariance is the variance of a 4-neighbour discrete Laplacian — the standard
// cheap focus measure. A sharp scene has strong local contrast and a high variance; a
// blurred, covered or blank one has almost none.
func laplacianVariance(grid []float64, side int) float64 {
	vals := make([]float64, 0, (side-2)*(side-2))
	at := func(x, y int) float64 { return grid[y*side+x] }
	for y := 1; y < side-1; y++ {
		for x := 1; x < side-1; x++ {
			l := at(x-1, y) + at(x+1, y) + at(x, y-1) + at(x, y+1) - 4*at(x, y)
			vals = append(vals, l)
		}
	}
	if len(vals) == 0 {
		return 0
	}
	var mean float64
	for _, v := range vals {
		mean += v
	}
	mean /= float64(len(vals))
	var variance float64
	for _, v := range vals {
		d := v - mean
		variance += d * d
	}
	return variance / float64(len(vals))
}

// FrameDifference is the mean absolute luma difference between two fingerprints, 0..1.
//
// Near zero means the picture has not changed at all — which on a live camera means the
// stream is frozen, not that the scene is calm. Even an empty room at night has sensor
// noise; a genuinely identical frame is a decoder or a source that has stopped.
func FrameDifference(a, b *Fingerprint) float64 {
	if a == nil || b == nil || len(a.Luma) != len(b.Luma) || len(a.Luma) == 0 {
		return 0
	}
	var sum float64
	for i := range a.Luma {
		sum += math.Abs(a.Luma[i] - b.Luma[i])
	}
	return sum / float64(len(a.Luma))
}

// HistogramDistance is the total variation distance between two luma histograms, 0..1.
//
// It is deliberately position-blind: a person crossing the frame barely moves the
// histogram, while a camera turned to face a wall changes the distribution of brightness
// across the whole picture. That is exactly the difference between activity and tampering.
func HistogramDistance(a, b *Fingerprint) float64 {
	if a == nil || b == nil || len(a.Histogram) != len(b.Histogram) {
		return 0
	}
	var sum float64
	for i := range a.Histogram {
		sum += math.Abs(a.Histogram[i] - b.Histogram[i])
	}
	return sum / 2
}

// LowLight reports whether a frame is dark enough that focus and contrast measures stop
// meaning anything.
//
// This is the guard that keeps the whole feature usable. At night a scene loses most of
// its edge energy legitimately, and under infrared it loses colour and contrast as well.
// Without this check every camera in the fleet would report a covered lens at dusk and be
// muted by the morning — after which none of them protect anything.
func LowLight(f *Fingerprint) bool {
	return f != nil && f.MeanLuma < LowLightMeanLuma
}

// LowLightMeanLuma is the brightness below which focus measures are not trusted.
const LowLightMeanLuma = 0.12

// Median returns the median of a slice, leaving the input untouched. Median rather than
// mean throughout: the reference is meant to describe the camera's NORMAL picture, and a
// mean is dragged around by exactly the abnormal readings this is trying to detect.
func Median(in []float64) float64 {
	if len(in) == 0 {
		return 0
	}
	cp := append([]float64(nil), in...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}
