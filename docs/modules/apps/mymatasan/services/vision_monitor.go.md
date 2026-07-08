# Module: apps/mymatasan/services/vision_monitor.go

## Purpose

Runs the MyMataSan background vision monitor that samples saved cameras and persists AI alert events.

## Responsibilities

- Poll detection rules on a configured fixed interval.
- Filter disabled rules, invalid camera IDs, and rules whose `schedulePolicy` is inactive.
- Group active rules by camera to avoid unnecessary frame captures.
- When `VisionMonitorSettings.Metadata` (a `*MetadataRecorder`) is set, sample the union of rule-bearing cameras and metadata-enabled cameras (`MetadataRecorder.EnabledCameras`) — a camera with metadata recording on but no alert rules is still captured and its frames run through an observe-only inference pass (`vision.ObservationCapable.ObserveOnly`) so the recorder logs what it saw; `MetadataRecorder.Observed` prevents a frame from being inferred twice when a rule `Detect` already ran on it. Metadata-only cameras write no alert snapshots (no rules, no detections to accompany).
- Capture JPEG frames from the saved RTSP URI when available, otherwise from the ONVIF snapshot URI.
- Forward every captured JPEG frame to the `recording.Manager` via `WriteFrame` so the ring buffer stays populated for pre-roll capture.
- Run the configured reusable `infra/vision` detector against each captured frame and active camera rule set.
- Persist detector results as alert events.
- Publish each actionable alert as a notification via `NotifyVisionAlert`, resolving the triggering rule name (`ruleNameByID`), attaching the captured JPEG frame as the snapshot image, and applying the runtime `vision.alertNotification` field-inclusion config read from settings per sample.
- On a successful alert creation, call `recording.Manager.TriggerEvent(cameraId, alertId, detection.FrameCapturedAt)` to start post-roll clip collection anchored to when the frame was captured, not when the detector finished processing it. This eliminates the YOLO latency shift that previously caused recordings to capture empty frames after the subject had already left.
- Emit throttled diagnostic alert events for capture failures, detector failures, and successful samples with no threshold-crossing detection.

## LPR capture path

When any rule for a camera has `detectionType = "lpr"`, `sampleCamera` sets `wantLPR = true` (via `rulesContainLPR`). This has two effects: (1) `captureFrame` calls `DetectionSource.CaptureForLPR` instead of `Capture`, which forces standalone mode and grabs a full-resolution (default 1920 px wide) frame — bypassing the low-res siphon frame that would make plates unreadable; (2) `Frame.WantLPR` is set so the persistent worker runs its OCR stage. Non-LPR cameras are never affected: the OCR path and high-res capture never run for them.

## Sampled-diagnostic suppression

The `"sampled"` heartbeat diagnostic (frame captured; nothing detected) is now only written when `persistSampledDiagnostics` is `true` (off by default). Capture and detect failures are still written regardless. This prevents the noisy heartbeat from bloating the `alert_event` table; the setting is exposed in `VisionMonitorSettings.PersistSampledDiagnostics` and in the config as `vision.persistSampledDiagnostics`.

## Notes

- The `recording.Manager` pointer is optional; when nil, both `WriteFrame` and `TriggerEvent` calls are skipped and recording is disabled for all cameras.
- The default interval is two seconds, and the default capture timeout is twelve seconds.
- RTSP frame capture uses runtime decoder settings, including ffmpeg path, RTSP transport, hardware decode mode/device, optional decoder name, probe/analyze limits, low-latency flags, MJPEG quality, and thread count.
- Snapshot fetches include saved camera credentials when present.
- The monitor receives a `vision.Detector` from app startup, so motion-only, external-object, and hybrid detectors share the same capture and persistence path.
- Diagnostic alert throttling is configurable through app startup settings.
- For persistent YOLO mode, a successful diagnostic with no alert usually means candidates did not pass the active rule schedule, class map, zone, threshold, min-frame, or cooldown checks.
