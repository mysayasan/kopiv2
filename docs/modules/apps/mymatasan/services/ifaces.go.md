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
  - `VerifyDeviceCredentials(ctx, detail, credentials)` — checks credentials against a not-yet-saved discovered camera (the Add flow, before there is a DB id) by resolving the ONVIF stream URI and/or probing RTSP `DESCRIBE`. Returns `"ok"`/`"unauthorized"`/`"unreachable"`; never returns an error for a successful *determination* (only for genuinely malformed input).
  - `CameraAuthStatus(ctx, id)` — verifies a saved camera's *stored* credentials the same way; this is the signal the camera node's access gate polls to decide whether to block the UI and prompt for new credentials.
  - `ListCameraUsers` / `CreateCameraUser` / `DeleteCameraUser` — manage the camera's local ONVIF user accounts (Device Management `GetUsers`/`CreateUsers`/`DeleteUsers`). `DeleteCameraUser` refuses to delete the account the app itself authenticates with (would lock the app out of the camera).
  - `RebootCamera(ctx, id)` — ONVIF `SystemReboot`; returns the device's reboot message.
  - `FactoryDefaultCamera(ctx, id, hard)` — ONVIF `SetSystemFactoryDefault` (`Soft` keeps network config, `Hard` wipes it).
  - `GetCameraDateTime` / `SetCameraDateTime` — read/write the camera clock (`GetSystemDateAndTime`+`GetNTP` merged on read; `SetSystemDateAndTime` + a follow-up `SetNTP` call in NTP mode on write).
  - `GetCameraNetwork` / `SetCameraNetwork` — read/write a camera NIC's IPv4 config, default gateway, and DNS via ONVIF `GetNetworkInterfaces`/`SetNetworkInterfaces`/`SetNetworkDefaultGateway`/`SetDNS`.
  - `GetCameraCapabilities(ctx, id)` — returns `CameraCapabilities`: service-level flags (Media/PTZ/Imaging/Analytics/Events) from `GetServices`, plus per-operation flags (`UserMgmt`/`DateTime`/`Network`) established by actually probing each read call, since Device-Management operations all live in the one mandatory device service and `GetServices` can't tell them apart. The UI hides management boxes the camera's firmware doesn't implement.
  - `GetCameraDeviceInfo(ctx, id)` — returns `CameraDeviceInfo` for the Live View → Camera Information panel: manufacturer/model/firmware/hardware/serial/location (static, from the stored detail; location is parsed out of ONVIF scopes) plus MAC address (from `GetNetwork`) and ONVIF version (from `GetServices`), both fetched live and best-effort so a slow/offline camera never blocks the response.
  - `PreviewSource(ctx, id, rtspUrl)` — resolves an arbitrary detected-profile RTSP URL into a playable `SnapshotSource` using the camera's stored credentials, **without persisting**, so a live preview of a specific stream (e.g. from Discover) never disturbs the camera's active RTSP URL that recording/detection rely on.
  - `TestStreamURL(ctx, id, rtspUrl)` — probes a specific detected-profile RTSP URL with the camera's credentials but, unlike `TestStream`, does **not** persist the resolved URL/status/tracks — a non-destructive per-stream connectivity check.
  - `TalkCapability(ctx, id)` — returns `TalkCapabilityResult` reporting whether the camera supports two-way audio (talk-back), over which transport (ONVIF backchannel or TP-Link Tapo/VIGI port-8800), and whether a speaker password is needed/already stored; cached (10 min TTL, `HasPassword` always read live), safe for cheap UI polling.
  - `SaveTalkPassword(ctx, id, password)` — stores the speaker/cloud password used by the TP-Link talk transport and invalidates the cached capability so `HasPassword` refreshes immediately.
  - `OpenTalkSession(ctx, id) (talk.Session, error)` — opens a live talk-back audio session to the camera speaker over its resolved transport (ONVIF backchannel or TP-Link Tapo/VIGI); caller must `Close` it.
- `ILocalUserService`
  - `EnsureDefaultAdmin(ctx, username, password) (AdminSeedResult, error)` seeds the first standalone admin account from `localAuth` config values (env `LOCAL_ADMIN_PASSWORD` overrides the password; when none is supplied a strong per-install password is generated). Returns `AdminSeedResult{Seeded, Username, Password, Generated}` so the caller can reveal the bootstrap login (first-run console banner + recovery file).
  - `ResetAdmin(ctx, username, password) (AdminSeedResult, error)` force-resets the admin password to a bootstrap credential (locked-out recovery; e.g. the installer's "reset admin login" reinstall). Must-change; returns the credential to reveal, same `AdminSeedResult` contract as seeding.
  - `Authenticate(ctx, username, password)` validates Basic Auth credentials
  - CRUD and password reset operations for Settings user management
- `IVisionService`
  - `GetRules(ctx, limit, offset)` and `SaveRule(ctx, req, userId)` for detection rule management
  - `DeleteRule(ctx, id)` for removing stale rules
  - `GetAlerts(ctx, limit, offset, cameraId, status, filters, sorters)` — paginated alert list. `cameraId` and `status` remain mandatory base constraints; `filters`/`sorters` (`[]sqldataenums.Filter`/`[]sqldataenums.Sorter`) come straight from the client `DataTable` (server mode) so the grid's column filters and sort run as true DB-side `WHERE`/`ORDER BY` clauses instead of a client-side slice. Defaults to `CreatedAt DESC` when no sort is supplied.
  - `CreateAlert(ctx, req, userId)` for alert event persistence
  - `AcknowledgeAlert(ctx, id, userId)` for operator acknowledgement
  - `PurgeAlerts(ctx, olderThan, onlyDiagnostics)` — delete rows and unlink snapshots for alerts older than a unix-second cutoff
  - `PurgeAlertsOlderThanDays(ctx, days, onlyDiagnostics)` — convenience wrapper that converts N days to a cutoff and calls `PurgeAlerts`
- `INotificationService` (selected)
  - `List(ctx, limit, offset, cameraId, unreadOnly, category, source)` — paginated notification feed
  - `Stats(ctx, from, to, bucketSeconds, tzOffsetSec)` — returns `*notification.Stats`, the aggregated dashboard payload (bucketed counts/breakdowns) over `[from, to]` unix-second window; `bucketSeconds` selects hour/day buckets and `tzOffsetSec` aligns bucket boundaries to the viewer's local clock. Backs `GET /api/notifications/stats`.
- `INotificationSettingsService` (selected)
  - `Get(ctx)` / `Save(ctx, settings)` — full notification settings blob (destinations, retention, legacy singletons)
  - `SaveDestination(ctx, dest)` — upserts one destination (create when `dest.Id` is empty, else replace by id) via read-modify-write against the persisted settings; other destinations and retention are untouched. Returns the saved destination and full settings.
  - `DeleteDestination(ctx, id)` — removes one destination by id; unknown id is a no-op. Returns full settings.
  - `SaveRetention(ctx, retention)` — persists only the retention section, leaving destinations and legacy singletons untouched. Returns full settings.
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
  - `PurgeOldestSegments(ctx, keepAfter, wantBytes)` — deletes the oldest recorded segments across all cameras regardless of per-camera retention, oldest `StartedAt` first, stopping once roughly `wantBytes` have been freed; segments starting at or after `keepAfter` (unix seconds) are never touched. Returns `(deletedCount, bytesFreed, error)`. Backs the machine-health monitor's disk-mitigation "overwrite oldest" mode.

## Key Request Types

- `CreateCameraUserRequest` — `Username`/`Password`/`UserLevel` for adding a local ONVIF user account on the camera.
- `SetCameraDateTimeRequest` — `DateTimeType` (`"Manual"`|`"NTP"`), `DaylightSavings`, `TimeZone`, `UTCDateTime` (RFC3339 UTC, required for Manual), `NTPFromDHCP`, `NTPServers`.
- `SetCameraNetworkRequest` — `InterfaceToken`, `DHCP`, `IPAddress`, `PrefixLength`, `Gateway`, `DNS`.
- `CameraCapabilities` — `Onvif`/`PTZ`/`Media`/`Imaging`/`Analytics`/`Events` service flags plus per-operation `UserMgmt`/`DateTime`/`Network` flags and static `Manufacturer`/`Model`/`FirmwareVersion`/`SerialNumber`/`HardwareID`.
- `CameraDeviceInfo` — the read-only device identity surfaced in Live View → Camera Information: `Manufacturer`, `Model`, `FirmwareVersion`, `HardwareID`, `SerialNumber`, `Location` (parsed from ONVIF scopes), `MACAddress`, `ONVIFVersion`, `ONVIFUri`.
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
