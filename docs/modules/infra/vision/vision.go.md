# Module: infra/vision/vision.go

## Purpose

Defines app-neutral contracts for visual detection rules, detector outputs, camera frames, alert events, and detector backends.

## Responsibilities

- Provide reusable request and normalized model shapes for detection rules.
- Provide reusable request and normalized model shapes for alert events.
- Define `Frame` as the camera image payload handed to detector implementations.
- Define `Detection` as the detector result before app-specific persistence.
- Define `Detector`, `ObjectDetector`, and `AlertSink` interfaces so apps can plug in different detector implementations and event sinks.
- Normalize rule defaults for threshold, minimum frames, cooldown, names, and detection type casing.
- Validate rule and alert JSON fields before app persistence.

## Detection.FrameCapturedAt

`Detection` now carries a `FrameCapturedAt int64` field (Unix seconds) set by every `Detect` implementation to the timestamp of the **input frame**, not the time the detection logic completed. This field is used by the recording manager to anchor the pre-roll/post-roll clip window to when the subject was actually visible rather than when the detector (e.g. YOLO) finished processing the frame.

## WantLPR / WantFace / WantAppearance frame gates

`Frame` carries a `WantLPR bool`, a `WantFace bool`, and a `WantAppearance bool` (all JSON `"-"`, never serialized). The vision monitor sets `WantLPR`/`WantFace` to `true` only for cameras that have an active LPR rule / face rule respectively, and both force the same high-resolution capture path (`CaptureForLPR`) since plates and faces both need real pixels, not the low-res object-detection frame. The persistent worker reads the `lpr`/`face` fields in the JSON request (forwarded by `persistent.go`) and runs the plate-localization + OCR stage, or the face detect+embed+gallery-match stage, only when asked — so the expensive OCR/face-embedding paths never run on cameras that don't need them.

`WantAppearance` (W3-2, "find more like this" — `apps/mymatasan/services/appearance_search.go.md`) is set per-camera (not per-rule, unlike the other two) whenever that camera has appearance search turned on. It requests an appearance vector on each eligible person/vehicle detection, so those sightings can later be ranked by how much they look like one an operator picked. It is the same per-camera compute gate as the two above — a forward pass per crop, and a camera nobody searches by appearance should not pay for one — but deliberately does **not** force the high-resolution capture path: LPR/face are targeted stages active only while a rule fires, while this one runs on every sampled frame of an enabled camera, so raising the baseline capture resolution for it would quietly multiply the cost of the whole sampling loop.

## Notes

- Supported detection type constants include `fire`, `smoke`, `person`, `vehicle`, `animal`, `crowd`, `intrusion`, `line_crossing`, `multi_line_crossing`, `lpr` (license-plate recognition), and `face` (face recognition).
- `DetectionLicensePlate = "lpr"` is the constant for license-plate rules; its validation is in `lpr.go` via `validateLPRRule`.
- `DetectionFace = "face"` is the constant for face-recognition rules; its validation is in `face.go` via `validateFaceRule`. The worker detects/embeds/matches faces against the globally enrolled gallery; the rule only decides which recognized/unknown faces are alert-worthy (known/include/unknown match modes) — see `face.go.md`.
- The package deliberately does not depend on MyMataSan entities or database code.
- Rule schedules are validated through `schedule.go`; detector implementations can assume validated rule inputs when called from app services.
- Object detector candidates are converted into persisted alerts by `object.go`, while motion-only rules remain available through `motion.go`.
- `Detection.FrameCapturedAt` is stamped in each `Detect` method after the detection loop so helper functions (`buildLineCrossingDetection`, etc.) do not need access to the frame directly.
