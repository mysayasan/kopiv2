# Module: apps/mymatasan/app/app.go

## Purpose

Implements the `mymatasan` app module for the shared runtime host.

## Responsibilities

- Provides app identity and base directory.
- Registers app entities for bootstrap schema generation.
- Registers built-in and config-driven seeders.
- Wires app-specific APIs (`onvif`, `settings`, `vision`, `recording`).
- Mounts app-specific APIs behind standalone DB-backed local Basic Auth.
- Seeds the first local admin user (`admin` / `Admin123`) when no local users exist.
- Owns the app-local stream manager used by WebRTC live view and closes it during graceful shutdown.
- Wires SQLite-backed runtime settings seeded from `decoder` and `stream` config defaults.
- Builds the app-local vision detector from `vision.detector` config and starts the monitor worker when `vision.enabled` allows it.
- Initialises the `recording.Manager` and applies all enabled `RecordingConfig` rows at startup via `Manager.Configure`.
- Reads the runtime `recording.storage` settings at startup: sizes the shared NVENC semaphore via `recording.SetNVENCConcurrency(maxConcurrentEncodes)` before any recorder starts, and seeds each recorder's `RecordCodec`/`RecordQuality` (at-rest codec) from it.
- RTSP URI resolution order at startup: `cfg.StreamURL` override → ONVIF `SnapshotSource` fallback. `cfg.FallbackStreamUrl` is passed as `FallbackRTSPURI`.
- Passes the `recording.Manager` pointer to `VisionMonitorSettings.Recorder` so alert events automatically trigger clip extraction.
- Registers `recorderManager.Close()` in the graceful shutdown func.
- Provides API docs metadata and endpoint descriptions for shared Swagger/OpenAPI output.
- Uses the embedded app version as the OpenAPI info version when available.

## Alert-log retention background job

A goroutine started at startup periodically purges the `alert_event` table (default every 6 hours, configurable via `vision.alertPurgeIntervalHours`). It runs once immediately at startup, then on the ticker:
- When `vision.diagnosticRetentionDays > 0`, calls `PurgeAlertsOlderThanDays(days, onlyDiagnostics=true)` to trim Vision-monitor diagnostic rows without touching real detections.
- When `vision.alertRetentionDays > 0`, calls `PurgeAlertsOlderThanDays(days, onlyDiagnostics=false)` to also trim real detection alerts.
Both paths unlink snapshot image files for removed rows.

## LPR model pointer

At startup, `lpr_model.txt` (alongside `active_model.txt` and `stock_model.txt` in the training dir) is resolved and its absolute path is written to `MYMATASAN_LPR_MODEL_FILE`. The YOLO worker reads this env var to know where to load the plate-detector weights. When the file is absent or empty, the LPR OCR stage never runs.

## Notes

- Only the public shared version API is mounted for this standalone app.
- Shared login, user/group/role, app-registry, endpoint, endpoint-RBAC, file-storage, log, runtime-log, and cache-service route groups are disabled.
- App entity registration includes `OnvifDevice`, `RuntimeSetting`, `LocalUser`, `DetectionRule`, `AlertEvent`, `RecordingSegment`, and `RecordingConfig`.
- OpenAPI endpoint discovery is automatic; this module enriches summaries/descriptions via `APIDocs()`.
- Vision detector modes are `motion`, `external`, `hybrid`, and `persistent`; `persistent` keeps one detector worker process alive and closes it during app shutdown.
- At startup, per-camera recording configs with a missing RTSP URI are skipped with a warning log; recording starts only for cameras where an RTSP URI can be resolved.
- `PersistSampledDiagnostics` from `vision.persistSampledDiagnostics` is forwarded to `VisionMonitorSettings`; when false (default), the noisy per-frame heartbeat diagnostic is suppressed and only capture/detect failures are written.
