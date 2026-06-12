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
  - `Stream(ctx)` and `Decoder(ctx)` for focused runtime reads
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
- `VisionMonitorSettings` — carries startup-only monitor enablement, interval, capture timeout, diagnostic cooldown, detector implementation, and a `*recording.Manager` pointer.
- `RuntimeSettings` — carries runtime-editable decoder and stream settings.

## Why It Matters

- Keeps handlers and service implementations loosely coupled.
- Allows swapping/testing ONVIF and RTSP concrete implementations.
- Keeps local login independent from MyIDSan identity/RBAC services.
- Keeps AI rule and alert APIs behind reusable vision contracts instead of binding detector logic to handlers.
