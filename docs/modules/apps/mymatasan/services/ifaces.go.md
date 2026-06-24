# Module: apps/mymatasan/services/ifaces.go

## Purpose

Declares service contracts for app-specific domain.

## Interfaces

- `IOnvifDeviceService`
  - `Discover(ctx, timeoutMs)` for WS-Discovery scans
  - `Probe(ctx, address)` for manual IP or device-service URL checks
  - CRUD/upsert operations for saved ONVIF device entities
  - `StreamOptions(ctx, id, credentials)` for listing every ONVIF media profile and RTSP URI; passes empty credentials to fall back to stored device credentials
  - `ResolveStream(ctx, id, req)` for saving the preferred or selected ONVIF media profile as the camera RTSP URI
  - `ResolveLiveView(ctx, id, credentials)` for resolving the live-view URI without changing the recording stream
  - `TestStream(ctx, id)` for RTSP DESCRIBE/SETUP probing
- `IRuntimeSettingsService`
  - `Get(ctx)` and `Save(ctx, settings)` for SQLite-backed runtime settings
  - `Reset(ctx)` to restore startup config defaults
  - `Stream(ctx)`, `Decoder(ctx)`, and `Recording(ctx)` for focused runtime reads
- `ICameraService` (selected)
  - `GetCameraEncoder(ctx, id)` — reads the camera's current ONVIF video encoder config
  - `ApplyCameraEncoder(ctx, id, ApplyCameraEncoderRequest)` — pushes a recording codec + bitrate cap to the camera's encoder via ONVIF (Phase 3 camera-side compression)
  - `LPRCapability(ctx, id)` — returns `LPRCapabilityResult` reporting whether the camera can supply plate-legible frames; when ONVIF profiles are readable it also surfaces the highest-resolution profile's RTSP URL for auto-pick capture. Cached (15 min TTL); safe to call on the per-frame path. Cache is invalidated on camera save.
- `ILocalUserService`
  - `EnsureDefaultAdmin(ctx)` seeds the first standalone admin account
  - `Authenticate(ctx, username, password)` validates Basic Auth credentials
  - CRUD and password reset operations for Settings user management
- `IVisionService`
  - `GetRules(ctx, limit, offset)` and `SaveRule(ctx, req, userId)` for detection rule management
  - `DeleteRule(ctx, id)` for removing stale rules
  - `GetAlerts(ctx, limit, offset, cameraId, createdAfter, createdBefore)` — paginated alert list with optional server-side filtering by camera ID and unix-timestamp date range
  - `CreateAlert(ctx, req, userId)` for alert event persistence
  - `AcknowledgeAlert(ctx, id, userId)` for operator acknowledgement
  - `PurgeAlerts(ctx, olderThan, onlyDiagnostics)` — delete rows and unlink snapshots for alerts older than a unix-second cutoff
  - `PurgeAlertsOlderThanDays(ctx, days, onlyDiagnostics)` — convenience wrapper that converts N days to a cutoff and calls `PurgeAlerts`
- `ITrainingService` (selected additions)
  - `GetLPRModel(ctx)` — returns `LPRModelInfo` (current plate-detector path + catalog + OCR readiness).
  - `SetLPRModel(ctx, value, userId)` — select a plate model by catalog name, https URL, local .pt path, or `""` / `"none"` to disable.
  - `ImportLPRModel(ctx, name, weights, userId)` — store an uploaded .pt and activate it; mirrors `ImportModel` but targets the LPR slot.
  - `DeactivateLPRModel(ctx, userId)` — clears the plate-model pointer and reloads the worker.
  - `StartLPRDepsSetup(ctx)` — pip-installs `easyocr + opencv-python + numpy` into the app's Python, streaming progress to the shared installer log (poll via `DepsSetupStatus`).

- `IRecordingService` (also implements `recording.SegmentSink`)
  - `GetSegments(ctx, limit, offset, cameraId, alertId, startedAfter, startedBefore)` — paginated clip list with optional camera, alert, and time-range filters
  - `GetSegmentById(ctx, id)` — fetch one clip row by ID
  - `SaveSegment(ctx, seg recording.SegmentResult)` — called by the infra recorder after a clip is written; satisfies `SegmentSink`
  - `DeleteSegment(ctx, id)` — removes the DB row and the file on disk
  - `ListConfigs(ctx)` — all per-camera recording configs
  - `GetConfig(ctx, cameraId)` — config for one camera; returns nil when none exists
  - `SaveConfig(ctx, req SaveRecordingConfigRequest)` — upsert by camera ID
  - `PurgeOldSegments(ctx)` — removes clips older than `RetentionDays` for each enabled config

## Key Request Types

- `SaveRecordingConfigRequest` — carries `CameraId`, `Enabled`, `PreRollSec`, `PostRollSec`, `StoragePath`, `RetentionDays`, `SegmentMinutes`, `StreamURL` (recording stream override), `FallbackStreamUrl` (fallback RTSP URI).
- `VisionMonitorSettings` — carries startup-only monitor enablement, interval, capture timeout, diagnostic cooldown, `PersistSampledDiagnostics` flag, detector implementation, and a `*recording.Manager` pointer.
- `RuntimeSettings` — carries runtime-editable decoder, stream, vision, and recording settings.
- `RecordingSettings` / `RecordingStorageSettings` — at-rest recording storage: `Storage.Codec` (`copy`/`h264`/`hevc`), `Storage.Quality` (NVENC CQ), `Storage.MaxConcurrentEncodes` (shared NVENC session cap). Default codec `copy`.
- `ApplyCameraEncoderRequest` — `Encoding` (`h264`/`h265`) + `BitrateLimitKbps` (≤0 keeps the camera's current bitrate) pushed to the camera encoder.
- `VisionSettings` — `Yolo` inference overrides, `Capture` frame-sourcing config (including `Capture.LPR.FrameWidth` for the high-res plate capture path), and `AlertNotification *AlertNotificationSettings`.
- `AlertNotificationSettings` — which detection-alert fields/media (`includeRuleName`, `includeLabel`, `includeConfidence`, `includeBoundingBox`, `includeZonePolygon`, `includeSnapshot`) populate the notification payload. Pointer: nil = include everything.
- `VisionAlertOptions` — extra per-alert notification context (`RuleName`, `Snapshot []byte`, `Fields *AlertNotificationSettings`) passed to `NotifyVisionAlert`.
- `LPRCapabilityResult` — `{supported, onvif, width, height, rtspUrl, detail}` describing whether a camera can supply plate-legible frames and (for ONVIF cameras) the highest-resolution profile's RTSP URL for auto-pick capture.
- `CaptureLPRSettings` — `{frameWidth int}` tuning the standalone high-res frame grabbed for LPR cameras (default 1920 px).

## Why It Matters

- Keeps handlers and service implementations loosely coupled.
- Allows swapping/testing ONVIF and RTSP concrete implementations.
- Keeps local login independent from MyIDSan identity/RBAC services.
- Keeps AI rule and alert APIs behind reusable vision contracts instead of binding detector logic to handlers.
