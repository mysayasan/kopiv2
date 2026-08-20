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
	if a == nil || b == nil {
		return 0
	}
	return histogramL1(a.Histogram, b.Histogram)
}

// HistogramDistanceFrom compares a frame against a bare REFERENCE histogram — the shape
// this camera's picture usually has — rather than against another single frame.
//
// The distinction is the whole point. Comparing two adjacent frames answers "did the
// picture just change", which is true for exactly one sample after a camera is re-aimed
// and false forever after, because the new view is as stable as the old one was. Comparing
// against a remembered normal answers "is the picture different from what this camera
// shows", which stays true for as long as the camera is pointing somewhere else — and that
// is the question a debounce can be applied to.
func HistogramDistanceFrom(ref []float64, f *Fingerprint) float64 {
	if f == nil {
		return 0
	}
	return histogramL1(ref, f.Histogram)
}

// histogramL1 is half the L1 distance between two normalized histograms: 0 when identical,
// 1 when they share no mass at all.
func histogramL1(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var sum float64
	for i := range a {
		sum += math.Abs(a[i] - b[i])
	}
	return sum / 2
}

// MedianHistogram reduces a window of recent histograms to the one that describes this
// camera's normal picture: the per-bucket median, renormalized to sum to 1.
//
// Median per bucket rather than mean, for the same reason the edge-energy baseline is a
// median: a mean is dragged around by exactly the abnormal readings this is meant to
// measure against, so a few frames of something across the lens would move the reference
// toward the thing being detected.
//
// The renormalization is not cosmetic. Per-bucket medians do not generally sum to 1, and
// histogramL1 assumes both sides do — an un-normalized reference silently scales every
// distance and would make the configured threshold mean something different on every
// camera.
func MedianHistogram(window [][]float64) []float64 {
	if len(window) == 0 {
		return nil
	}
	buckets := 0
	for _, h := range window {
		if len(h) > buckets {
			buckets = len(h)
		}
	}
	if buckets == 0 {
		return nil
	}
	out := make([]float64, buckets)
	column := make([]float64, 0, len(window))
	var total float64
	for i := 0; i < buckets; i++ {
		column = column[:0]
		for _, h := range window {
			if i < len(h) {
				column = append(column, h[i])
			}
		}
		out[i] = Median(column)
		total += out[i]
	}
	if total <= 0 {
		return out
	}
	for i := range out {
		out[i] /= total
	}
	return out
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
