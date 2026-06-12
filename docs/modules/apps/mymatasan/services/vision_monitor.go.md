# Module: apps/mymatasan/services/vision_monitor.go

## Purpose

Runs the MyMataSan background vision monitor that samples saved cameras and persists AI alert events.

## Responsibilities

- Poll detection rules on a configured fixed interval.
- Filter disabled rules, invalid camera IDs, and rules whose `schedulePolicy` is inactive.
- Group active rules by camera to avoid unnecessary frame captures.
- Capture JPEG frames from the saved RTSP URI when available, otherwise from the ONVIF snapshot URI.
- Forward every captured JPEG frame to the `recording.Manager` via `WriteFrame` so the ring buffer stays populated for pre-roll capture.
- Run the configured reusable `infra/vision` detector against each captured frame and active camera rule set.
- Persist detector results as alert events.
- On a successful alert creation, call `recording.Manager.TriggerEvent(cameraId, alertId, detection.FrameCapturedAt)` to start post-roll clip collection anchored to when the frame was captured, not when the detector finished processing it. This eliminates the YOLO latency shift that previously caused recordings to capture empty frames after the subject had already left.
- Emit throttled diagnostic alert events for capture failures, detector failures, and successful samples with no threshold-crossing detection.

## Notes

- The `recording.Manager` pointer is optional; when nil, both `WriteFrame` and `TriggerEvent` calls are skipped and recording is disabled for all cameras.
- The default interval is two seconds, and the default capture timeout is twelve seconds.
- RTSP frame capture uses runtime decoder settings, including ffmpeg path, RTSP transport, hardware decode mode/device, optional decoder name, probe/analyze limits, low-latency flags, MJPEG quality, and thread count.
- Snapshot fetches include saved camera credentials when present.
- The monitor receives a `vision.Detector` from app startup, so motion-only, external-object, and hybrid detectors share the same capture and persistence path.
- Diagnostic alert throttling is configurable through app startup settings.
- For persistent YOLO mode, a successful diagnostic with no alert usually means candidates did not pass the active rule schedule, class map, zone, threshold, min-frame, or cooldown checks.
