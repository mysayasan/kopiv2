# Module: infra/vision/vision.go

## Purpose

Defines app-neutral contracts for visual detection rules, detector outputs, camera frames, alert events, and detector backends.

## Responsibilities

- Provide reusable request and normalized model shapes for detection rules.
- Provide reusable request and normalized model shapes for alert events.
- Define `Frame` as the camera image payload handed to detector implementations.
- Define `Detection` as the detector result before app-specific persistence.
- Define `Detector` and `AlertSink` interfaces so apps can plug in different detector implementations and event sinks.
- Normalize rule defaults for threshold, minimum frames, cooldown, names, and detection type casing.
- Validate rule and alert JSON fields before app persistence.

## Notes

- Supported detection type constants include `fire`, `smoke`, `person`, `vehicle`, and `intrusion`.
- The package deliberately does not depend on MyMataSan entities or database code.
- Rule schedules are validated through `schedule.go`; detector implementations can assume validated rule inputs when called from app services.
