# Module: infra/vision/tamper.go

## Purpose

The pure-arithmetic half of camera tamper detection: is this camera still showing what it is supposed to show?

Reachability monitoring answers "does it respond"; continuity monitoring answers "did it write footage". Both report green for a covered lens, a camera turned to face a wall, or a defocused ring, because the camera still answers and the recorder still writes files — just of a wall. This file is the third question, expressed as a few hundred lines of arithmetic over a downsampled luma grid so it needs no ML and no dependency on the detector. The stateful judgement — "is this reading abnormal FOR THIS CAMERA" — deliberately does not live here; it lives in `apps/mymatasan/services/camera_tamper_monitor.go`, because that needs history and history needs somewhere to live. Everything here is a pure function over a `Fingerprint`, which is what makes it testable without a camera.

## Responsibilities

- `Fingerprint` — the cheap, comparable summary of one frame: a 32×32 grayscale grid (`Luma`), its mean brightness (`MeanLuma`), a focus measure (`EdgeEnergy`, the variance of a discrete Laplacian), and a 16-bucket luma histogram.
- `NewFingerprint(frame []byte)` — decodes a JPEG (falling back to the `jpeg` package directly if the generic `image.Decode` registry misses) and summarizes it via `FingerprintImage`.
- `FingerprintImage(img image.Image)` — nearest-neighbour downsamples to the fixed 32×32 grid (deliberately small: the question is "has the whole scene changed", not "what is in it", so a person walking through frame is a small perturbation while a hand over the lens is a total one) and computes all four fields.
- `FrameDifference(a, b *Fingerprint)` — mean absolute luma difference, 0..1. Near zero means the picture has not changed AT ALL, which on a live stream means frozen, not calm — even an empty room has sensor noise.
- `HistogramDistance(a, b *Fingerprint)` — total variation distance between two luma histograms, 0..1. Deliberately position-blind: a person crossing frame barely moves the histogram, a camera turned to a wall moves the whole distribution. Now a thin wrapper over `histogramL1`; its own behavior is unchanged.
- `HistogramDistanceFrom(ref []float64, f *Fingerprint)` — the same distance, but against a bare reference histogram rather than another frame's. This is what the monitor's `moved` verdict is actually measured against: comparing two adjacent frames answers "did the picture just change" (true for exactly one sample after a camera is re-aimed, false forever after), while comparing against a remembered normal answers "is the picture different from what this camera shows" — the question a debounce can be applied to. `nil`/empty input returns 0 rather than panicking, since a camera with no history yet is asked about on its very first sweep.
- `histogramL1(a, b []float64)` — the shared L1-distance helper both of the above reduce to.
- `MedianHistogram(window [][]float64)` — reduces a rolling window of recent histograms to the one that describes a camera's normal picture: the per-bucket median, renormalized to sum to 1 (per-bucket medians do not generally sum to 1, and an un-normalized reference would silently rescale every distance so the configured `MovedDistance` threshold meant something different on every camera). Median per bucket for the same reason as `Median` below: a mean is dragged toward exactly the abnormal readings this exists to detect against.
- `LowLight(f *Fingerprint)` / `LowLightMeanLuma` (0.12) — whether a frame is too dark for focus/contrast measures to mean anything. This is the guard that keeps the feature usable at all: without it, every camera reports a covered lens at dusk and is muted by morning.
- `Median(in []float64)` — median rather than mean, used throughout the monitor for the same reason: the reference must describe the camera's NORMAL picture, and a mean is dragged around by exactly the abnormal readings this is trying to detect.

## Notes

- No package-level state; every function takes its inputs explicitly, which is what lets `tamper_test.go` exercise the maths against generated scenes (sharp / blurred / flat / shifted / person-crossing) instead of checked-in binary fixtures.
- `fingerprintGrid` (32) and `histogramBuckets` (16) are unexported constants — changing either changes what "the same camera's normal picture" means for any site already running the monitor's rolling baseline, so it is not something to retune casually.
- `HistogramDistanceFrom`/`MedianHistogram` exist because `apps/mymatasan/services/camera_tamper_monitor.go`'s `moved` verdict originally compared each sample against the previous one and could never survive the monitor's `FailureThreshold` debounce (a re-aimed camera differs from its predecessor for exactly one sample, then matches it forever). The monitor now keeps a rolling window of histograms and measures each sample against `MedianHistogram` of that window via `HistogramDistanceFrom`, the same shape as the pre-existing edge-energy baseline.
- Consumed by `apps/mymatasan/services/camera_tamper_monitor.go` (`services/camera_tamper_monitor.go.md`), which reads the JPEG the recorder already siphons for the AI detector — no extra capture cost.
- Covered by `infra/vision/tamper_test.go`, including `MedianHistogram`'s normalization and majority-scene behavior and `HistogramDistanceFrom` matching `HistogramDistance` frame-to-frame.
