# Module: infra/vision/tamper.go

## Purpose

The pure-arithmetic half of camera tamper detection: is this camera still showing what it is supposed to show?

Reachability monitoring answers "does it respond"; continuity monitoring answers "did it write footage". Both report green for a covered lens, a camera turned to face a wall, or a defocused ring, because the camera still answers and the recorder still writes files — just of a wall. This file is the third question, expressed as a few hundred lines of arithmetic over a downsampled luma grid so it needs no ML and no dependency on the detector. The stateful judgement — "is this reading abnormal FOR THIS CAMERA" — deliberately does not live here; it lives in `apps/mymatasan/services/camera_tamper_monitor.go`, because that needs history and history needs somewhere to live. Everything here is a pure function over a `Fingerprint`, which is what makes it testable without a camera.

## Responsibilities

- `Fingerprint` — the cheap, comparable summary of one frame: a 32×32 grayscale grid (`Luma`), its mean brightness (`MeanLuma`), a focus measure (`EdgeEnergy`, the variance of a discrete Laplacian), and a 16-bucket luma histogram.
- `NewFingerprint(frame []byte)` — decodes a JPEG (falling back to the `jpeg` package directly if the generic `image.Decode` registry misses) and summarizes it via `FingerprintImage`.
- `FingerprintImage(img image.Image)` — nearest-neighbour downsamples to the fixed 32×32 grid (deliberately small: the question is "has the whole scene changed", not "what is in it", so a person walking through frame is a small perturbation while a hand over the lens is a total one) and computes all four fields.
- `FrameDifference(a, b *Fingerprint)` — mean absolute luma difference, 0..1. Near zero means the picture has not changed AT ALL, which on a live stream means frozen, not calm — even an empty room has sensor noise.
- `HistogramDistance(a, b *Fingerprint)` — total variation distance between two luma histograms, 0..1. Deliberately position-blind: a person crossing frame barely moves the histogram, a camera turned to a wall moves the whole distribution.
- `LowLight(f *Fingerprint)` / `LowLightMeanLuma` (0.12) — whether a frame is too dark for focus/contrast measures to mean anything. This is the guard that keeps the feature usable at all: without it, every camera reports a covered lens at dusk and is muted by morning.
- `Median(in []float64)` — median rather than mean, used throughout the monitor for the same reason: the reference must describe the camera's NORMAL picture, and a mean is dragged around by exactly the abnormal readings this is trying to detect.

## Notes

- No package-level state; every function takes its inputs explicitly, which is what lets `tamper_test.go` exercise the maths against generated scenes (sharp / blurred / flat / shifted / person-crossing) instead of checked-in binary fixtures.
- `fingerprintGrid` (32) and `histogramBuckets` (16) are unexported constants — changing either changes what "the same camera's normal picture" means for any site already running the monitor's rolling baseline, so it is not something to retune casually.
- Consumed by `apps/mymatasan/services/camera_tamper_monitor.go` (`services/camera_tamper_monitor.go.md`), which reads the JPEG the recorder already siphons for the AI detector — no extra capture cost.
- Covered by `infra/vision/tamper_test.go`.
