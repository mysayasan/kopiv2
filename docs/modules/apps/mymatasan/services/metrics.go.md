# Module: apps/mymatasan/services/metrics.go

## Purpose

Declares the app-specific runtime metric names mymatasan records — the numbers an operator needs to diagnose a site they cannot log into ("is the detector keeping up?", "is ffmpeg thrashing?", "are segments failing to finalize?", "are notifications being dropped?", "why did recording stop overnight?") — and registers their help text.

## Metrics

- `MetricInferenceDurationMs` = `mymatasan_inference_duration_ms` (histogram, `{camera}`) — how long one detector pass took, per camera. The first number to look at when detection "feels slow" or frames are being skipped. Timed on both the success and failure path (a detector failing slowly is the case worth seeing).
- `MetricFramesTotal` = `mymatasan_frames_total` (`{camera, outcome=ok|capture_failed|detect_failed}`) — counts sampled frames by outcome. A camera silently failing every capture looks identical to a quiet camera in the alert log; this counter tells them apart.
- `MetricAlertsTotal` = `mymatasan_alerts_total` (`{camera, kind=detection|diagnostic}`) — counts emitted alerts by kind, so a diagnostic flood is distinguishable from real activity.
- `MetricCameraOnline` = `mymatasan_camera_online` (gauge, `{camera}`) — `1` when a camera's last health probe succeeded, else `0`.
- `MetricCamerasOffline` = `mymatasan_cameras_offline` (gauge) — how many cameras are currently unreachable; the single number worth alerting on.
- `MetricDiskUsedPercent` = `mymatasan_disk_used_percent` (gauge, `{mount}`) — per-mount disk usage, including the recordings volume.
- `MetricRecordingPaused` = `mymatasan_recording_paused` (gauge) — `1` while the machine-health disk guard has recording paused. Footage is not being written while this is `1` — the most important single bit on the box.
- `MetricDiskMitigationTotal` = `mymatasan_disk_mitigation_total` (`{action=pause|resume|overwrite}`) — counts disk-guard actions taken.

`DescribeMetrics(m telemetry.Metrics)` registers help text for all eight; called once at startup from `app.go`.

## Naming convention

`kopiv2_*` is used by metrics emitted from shared infra (app-neutral — e.g. `infra/recording`, `infra/notification`; myiotsan will emit the same ones). `mymatasan_*` (this file) is used for metrics owned by this app specifically.

## Notes

- Every label here is bounded (a camera id, or a small fixed outcome/action enum) — the label discipline documented in `infra/telemetry/telemetry.go.md` and enforced (as a backstop, not a licence) by the cardinality cap in `infra/telemetry/prometheus/metrics.go.md`.
- Emitted by `apps/mymatasan/services/vision_monitor.go` (inference duration, frames, alerts), `apps/mymatasan/services/camera_health_monitor.go` (camera online/offline), and `apps/mymatasan/services/machine_health_monitor.go` (disk used percent, recording paused, disk mitigation).
- Consumers pass `nil` when telemetry is disabled or in tests; `DescribeMetrics` and every metric constant are safe to reference regardless (the recording is what's nil-safe, not the constants).
