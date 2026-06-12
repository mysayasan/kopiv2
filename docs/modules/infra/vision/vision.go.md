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

## Notes

- Supported detection type constants include `fire`, `smoke`, `person`, `vehicle`, `animal`, `intrusion`, `line_crossing`, and `multi_line_crossing`.
- The package deliberately does not depend on MyMataSan entities or database code.
- Rule schedules are validated through `schedule.go`; detector implementations can assume validated rule inputs when called from app services.
- Object detector candidates are converted into persisted alerts by `object.go`, while motion-only rules remain available through `motion.go`.
- `Detection.FrameCapturedAt` is stamped in each `Detect` method after the detection loop so helper functions (`buildLineCrossingDetection`, etc.) do not need access to the frame directly.
