package vision

import (
	"image"
	"image/color"
	"math"
	"math/rand"
	"testing"
)

// Scenes are generated rather than checked in as fixtures: the properties under test are
// mathematical (a blurred picture has less edge energy than a sharp one), and a generator
// states that intent far more clearly than a binary blob nobody can inspect in review.

// sharpScene is a busy picture: a checkerboard with texture, the kind of thing a camera
// pointed at a real room produces.
func sharpScene(seed int64) image.Image {
	rng := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			v := 40
			if (x/16+y/16)%2 == 0 {
				v = 210
			}
			v += rng.Intn(20) - 10
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			img.Set(x, y, color.Gray{Y: uint8(v)})
		}
	}
	return img
}

// blurredScene is the same picture through a box blur — a defocused ring, or a lens with
// something pressed against it.
func blurredScene(src image.Image, radius int) image.Image {
	b := src.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			var sum, n float64
			for dy := -radius; dy <= radius; dy++ {
				for dx := -radius; dx <= radius; dx++ {
					sx, sy := x+dx, y+dy
					if sx < b.Min.X || sx >= b.Max.X || sy < b.Min.Y || sy >= b.Max.Y {
						continue
					}
					r, g, bl, _ := src.At(sx, sy).RGBA()
					sum += (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 257.0
					n++
				}
			}
			out.Set(x, y, color.Gray{Y: uint8(sum / n)})
		}
	}
	return out
}

// flatScene is a uniform picture: a lens taped over, or a camera facing a blank wall.
func flatScene(level uint8) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.Set(x, y, color.Gray{Y: level})
		}
	}
	return img
}

// shiftedScene is the same scene panned — a camera that has been turned.
func shiftedScene(src image.Image, dx, dy int) image.Image {
	b := src.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			sx, sy := x+dx, y+dy
			if sx < b.Min.X || sx >= b.Max.X || sy < b.Min.Y || sy >= b.Max.Y {
				out.Set(x, y, color.Gray{Y: 0})
				continue
			}
			out.Set(x, y, src.At(sx, sy))
		}
	}
	return out
}

// personCrossing puts a dark blob over part of an otherwise unchanged scene — activity,
// which must NOT read as tampering.
func personCrossing(src image.Image) image.Image {
	b := src.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.Set(x, y, src.At(x, y))
		}
	}
	for y := 80; y < 200; y++ {
		for x := 100; x < 150; x++ {
			out.Set(x, y, color.Gray{Y: 15})
		}
	}
	return out
}

// --- focus / covered ---------------------------------------------------------

// The core of covered-lens detection: a blurred or blanked picture has dramatically less
// edge energy than a sharp one. Everything else in the monitor is about deciding when a
// drop is abnormal FOR THIS CAMERA; this is the measurement it decides on.
func TestEdgeEnergyCollapsesWhenTheViewIsObscured(t *testing.T) {
	sharp := FingerprintImage(sharpScene(1))
	blurred := FingerprintImage(blurredScene(sharpScene(1), 6))
	flat := FingerprintImage(flatScene(128))

	if sharp.EdgeEnergy <= blurred.EdgeEnergy {
		t.Fatalf("a blurred scene must have less edge energy: sharp=%v blurred=%v",
			sharp.EdgeEnergy, blurred.EdgeEnergy)
	}
	if blurred.EdgeEnergy <= flat.EdgeEnergy {
		t.Fatalf("a flat scene must have the least edge energy: blurred=%v flat=%v",
			blurred.EdgeEnergy, flat.EdgeEnergy)
	}
	// A taped lens is not a marginal reading — it must be a collapse, or a threshold
	// generous enough to avoid false positives would never catch it.
	if flat.EdgeEnergy > sharp.EdgeEnergy*0.05 {
		t.Fatalf("a covered lens should collapse edge energy to a small fraction: sharp=%v flat=%v",
			sharp.EdgeEnergy, flat.EdgeEnergy)
	}
}

// --- frozen ------------------------------------------------------------------

func TestFrameDifferenceIsZeroForAnIdenticalFrame(t *testing.T) {
	fp := FingerprintImage(sharpScene(7))
	if d := FrameDifference(fp, fp); d != 0 {
		t.Fatalf("an identical frame must differ by 0, got %v", d)
	}
}

// The signal that separates a frozen stream from a quiet scene. Even a still room has
// sensor noise frame to frame; a difference of exactly nothing means the source stopped.
func TestFrameDifferenceSeparatesAFrozenStreamFromAQuietScene(t *testing.T) {
	a := FingerprintImage(sharpScene(11))
	// Same scene, different noise — a live camera watching an empty room.
	quiet := FingerprintImage(sharpScene(12))
	frozen := FrameDifference(a, a)
	live := FrameDifference(a, quiet)

	if frozen != 0 {
		t.Fatalf("frozen difference = %v, want 0", frozen)
	}
	if live <= frozen {
		t.Fatalf("a live quiet scene must still differ from itself over time: live=%v frozen=%v", live, frozen)
	}
}

// --- moved -------------------------------------------------------------------

// A camera turned to face elsewhere changes the whole brightness distribution. A person
// walking through does not — and getting that distinction wrong is what would make the
// feature unusable, because people walking past is what cameras are FOR.
func TestSceneShiftDistinguishesAMovedCameraFromActivity(t *testing.T) {
	base := sharpScene(21)
	ref := FingerprintImage(base)
	moved := FingerprintImage(shiftedScene(blurredScene(flatScene(30), 1), 0, 0))
	activity := FingerprintImage(personCrossing(base))

	movedDist := HistogramDistance(ref, moved)
	activityDist := HistogramDistance(ref, activity)

	if movedDist <= activityDist {
		t.Fatalf("a moved camera must shift the histogram more than activity does: moved=%v activity=%v",
			movedDist, activityDist)
	}
	if activityDist > 0.25 {
		t.Fatalf("a person crossing frame moved the histogram by %v — that would alarm on normal activity", activityDist)
	}
}

func TestHistogramDistanceIsZeroForTheSameFrame(t *testing.T) {
	fp := FingerprintImage(sharpScene(31))
	if d := HistogramDistance(fp, fp); d != 0 {
		t.Fatalf("distance to itself = %v, want 0", d)
	}
}

// --- the night problem -------------------------------------------------------

// This guard is what decides whether the feature survives contact with a real site. At
// night a scene loses most of its edge energy legitimately; without a low-light check
// every camera in the fleet reports a covered lens at dusk and is muted by morning.
func TestLowLightIsDetectedSoFocusMeasuresCanBeIgnored(t *testing.T) {
	day := FingerprintImage(sharpScene(41))
	night := FingerprintImage(flatScene(8))

	if LowLight(day) {
		t.Fatalf("a normally-lit scene must not read as low light (mean luma %v)", day.MeanLuma)
	}
	if !LowLight(night) {
		t.Fatalf("a near-dark scene must read as low light (mean luma %v)", night.MeanLuma)
	}
}

// --- helpers -----------------------------------------------------------------

// Median, not mean: the reference is meant to describe the camera's NORMAL picture, and a
// mean is dragged around by exactly the abnormal readings this exists to detect.
func TestMedianIgnoresOutliers(t *testing.T) {
	steady := []float64{10, 10, 11, 10, 9, 10}
	withSpike := append(append([]float64(nil), steady...), 1000)
	if m := Median(withSpike); math.Abs(m-10) > 1 {
		t.Fatalf("median = %v, want ~10 — an outlier moved it", m)
	}
	if Median(nil) != 0 {
		t.Fatal("median of nothing must be 0")
	}
}

func TestFingerprintRejectsAnEmptyFrame(t *testing.T) {
	if _, err := NewFingerprint(nil); err == nil {
		t.Fatal("an empty frame must be an error, not a zero fingerprint")
	}
}

func TestFrameDifferenceIsSafeOnMismatchedInput(t *testing.T) {
	fp := FingerprintImage(sharpScene(51))
	if d := FrameDifference(fp, nil); d != 0 {
		t.Fatalf("nil comparison must be 0, got %v", d)
	}
	if d := HistogramDistance(nil, fp); d != 0 {
		t.Fatalf("nil comparison must be 0, got %v", d)
	}
}

// --- reference histogram -------------------------------------------------------

// MedianHistogram is what a camera's "normal picture" is reduced to, and every MOVED
// verdict is a distance from it. If it does not sum to 1 the distance is silently scaled,
// and the configured threshold means something different on every camera — a
// mis-calibration whose only symptom is alerts that are wrong everywhere by an amount
// nobody can see.
func TestMedianHistogramStaysNormalizedOnAMixedWindow(t *testing.T) {
	// Three genuinely different scenes, so each bucket's median comes from a different
	// one. The raw per-bucket medians here sum to 0.5, not 1 — which is the case a window
	// of near-identical frames would never expose, and exactly the case a camera watching
	// a changeable scene produces.
	a := []float64{1, 0, 0, 0}
	b := []float64{0, 1, 0, 0}
	c := []float64{0, 0, 1, 0}
	window := [][]float64{a, a, b, c}

	got := MedianHistogram(window)
	var sum float64
	for _, v := range got {
		sum += v
	}
	if math.Abs(sum-1) > 1e-9 {
		t.Fatalf("the reference must sum to 1, got %v (sum %v)", got, sum)
	}
	// And the scaling must be visible in the distance, which is what the threshold is
	// compared against: un-normalized, this reference is half the mass it should be and
	// every camera's distance is inflated toward the alert.
	if d := HistogramDistanceFrom(got, &Fingerprint{Histogram: a}); d > 1e-9 {
		t.Fatalf("the reference sits on the majority scene, so distance to it is 0; got %v", d)
	}
}

// The median has to describe the TYPICAL picture rather than the average of everything
// seen — otherwise a few frames of something across the lens drag the reference toward the
// very thing it exists to be measured against.
func TestMedianHistogramTracksTheMajorityScene(t *testing.T) {
	normal := []float64{0.8, 0.2, 0, 0}
	odd := []float64{0, 0, 0.1, 0.9}
	window := [][]float64{normal, normal, normal, normal, odd}

	ref := MedianHistogram(window)
	near := &Fingerprint{Histogram: normal}
	far := &Fingerprint{Histogram: odd}
	if d := HistogramDistanceFrom(ref, near); d > 0.05 {
		t.Fatalf("the reference should sit on the majority scene; distance to it was %v", d)
	}
	if d := HistogramDistanceFrom(ref, far); d < 0.8 {
		t.Fatalf("the odd scene should be far from the reference; got %v", d)
	}
}

// A distance against a reference must behave exactly like a distance against a frame, or
// one configured threshold means two different things.
func TestHistogramDistanceFromMatchesFrameToFrame(t *testing.T) {
	a := &Fingerprint{Histogram: []float64{0.5, 0.5, 0, 0}}
	b := &Fingerprint{Histogram: []float64{0, 0, 0.5, 0.5}}
	if got, want := HistogramDistanceFrom(a.Histogram, b), HistogramDistance(a, b); math.Abs(got-want) > 1e-9 {
		t.Fatalf("HistogramDistanceFrom = %v, HistogramDistance = %v", got, want)
	}
	if got := HistogramDistanceFrom(a.Histogram, a); got != 0 {
		t.Fatalf("a frame is not distant from itself: %v", got)
	}
	// Degenerate input must be quiet rather than panic: a camera with no history yet is a
	// normal camera, and the monitor asks about it on its very first sweep.
	if got := HistogramDistanceFrom(nil, b); got != 0 {
		t.Fatalf("no reference means no distance, got %v", got)
	}
	if got := MedianHistogram(nil); got != nil {
		t.Fatalf("no window means no reference, got %v", got)
	}
}
