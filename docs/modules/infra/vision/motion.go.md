# Module: infra/vision/motion.go

## Purpose

Provides the dependency-free reusable detector implementation: motion detection inside configured rule polygons.

## Responsibilities

- Decode JPEG frames into grayscale pixel buffers.
- Keep previous-frame state per camera.
- Parse normalized polygon JSON and fall back to the full frame when the polygon is missing or invalid.
- Compare consecutive frames using a configurable pixel delta and stride.
- Compute the changed-pixel ratio inside each rule polygon.
- Apply rule threshold, minimum frame count, and cooldown before returning detections.
- Emit detector metadata that includes the motion source and changed-frame ratio.

## Notes

- The detector is intentionally simple and local-device friendly for the MVP.
- It is safe for concurrent use; camera state is protected by a mutex.
- It implements the shared `Detector` interface, so it can run as the whole detector in `motion` mode or as the intrusion fallback in `hybrid` mode.
