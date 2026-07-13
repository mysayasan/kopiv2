# Module: apps/mymatasan/app/app.go

## Purpose

Implements the `mymatasan` app module for the shared runtime host.

## Responsibilities

- At the very top of `RegisterAppRoutes`, calls `infra/safego.SetLogger` to route recovered background-goroutine panics into the app logger (`deps.Logger.Warnf`), before any service is built — so a panic is never lost to stdout on a service install (until this runs, `safego` falls back to the standard logger, so nothing is ever silent either way).
- Provides app identity and base directory.
- Registers app entities for bootstrap schema generation.
- Registers built-in and config-driven seeders.
- Wires app-specific APIs (`onvif`, `settings`, `vision`, `recording`).
- Mounts app-specific APIs behind standalone DB-backed local Basic Auth.
- Seeds the first local admin user when no local users exist, via `localUserService.EnsureDefaultAdmin(ctx, deps.Config.LocalAuth.Username, deps.Config.LocalAuth.Password)` (generates a per-install password when config/env supply none; always flags the seeded account must-change). When the result reports `Seeded`, `announceFirstRunAdmin` reveals the bootstrap login: it **always** writes an `INITIAL_ADMIN_LOGIN.txt` recovery file (0600) to the data dir via `writeFirstRunCredentialFile` (the reliable place on Windows too, where a service console is invisible), and prints a console sign-in banner (URL via `firstRunConsoleURL` — https on a TLS port else http, first configured port, default 3000; username; saved-file path). The password is echoed in the banner only when it was generated (`Generated=true`); a config/env-supplied one is pointed at, not logged.
- One-shot admin reset: when the user table already exists (no seed), `app.go` checks for a `RESET_ADMIN` marker (`adminResetMarkerFile`) in the data dir — dropped by the Windows installer's "reset admin login" option alongside an injected `LOCAL_ADMIN_PASSWORD`. If present, it **deletes the marker first** (so a normal restart never re-runs the reset), then calls `localUserService.ResetAdmin(...)` and `announceFirstRunAdmin` with the result. `fileExists` guards the check.
- Owns the app-local stream manager used by WebRTC live view and closes it during graceful shutdown.
- Wires SQLite-backed runtime settings seeded from `decoder` and `stream` config defaults.
- Builds the app-local vision detector from `vision.detector` config and starts the monitor worker when `vision.enabled` allows it.
- When an object detector backend is present, warms it up in the background via `infra/safego.Go` (name `mymatasan.vision.warmup`, bounded by a 120s timeout) rather than a bare `go`: this is one-shot, so a panic during warmup is recovered and logged but not retried — live detection simply warms on its first real inference instead.
- Resolves the detector's Python worker script (`vision.detector.args`) to an absolute path against `deps.HomeDir` via `resolveDetectorScriptArgs`/`resolveDetectorScript`, so the worker is found regardless of the process working directory: a dev run from the repo root (`HomeDir=apps/mymatasan`) or the staged `bin/` bundle (`HomeDir=<exe dir>`, with `ai/` staged alongside). The default config uses the HomeDir-relative `ai/yolo_worker.py`; legacy repo-root-relative values (`./apps/mymatasan/ai/yolo_worker.py`) are recovered by basename in `<HomeDir>/ai` (they otherwise doubled to `<bin>/apps/mymatasan/ai/...` and the worker failed to open, killing detection/calibration). The training script + base-model paths are derived from the resolved worker directory, so they follow automatically.
- Initialises the `recording.Manager` and applies all enabled `RecordingConfig` rows at startup via `Manager.Configure`.
- Registers the camera-delete cleanup cascade: if `cameraService` implements `services.CameraDeletionCascade`, `app.go` calls `AddCameraCleanup` six times, in order — (1) stop the recorder and detect-only stream (`recorderManager.StopDetectionStream` + `Configure(..., Enabled: false)`), (2) `recordingService.PurgeAllForCamera`, (3) `observationService.PurgeAllForCamera`, (4) `visionService.DeleteRulesForCamera`, (5) `visionService.PurgeAlertsForCamera`, (6) `recordingService.DeleteConfigForCamera` (last, since the purges above are driven off that config row). `cameraService.Delete` runs these before deleting the camera row and aborts on the first failure, so a camera is never half-removed. Previously deleting a camera left its recorder running (ffmpeg still connected, still writing segments) and its detection rules alive (the vision monitor kept sampling a camera that no longer existed, logging a capture-failed diagnostic every interval) until the next restart — and left its footage/config permanently stranded since retention is driven off a recording config that used to survive the camera row.
- Reads the runtime `recording.storage` settings at startup: sizes the shared NVENC semaphore via `recording.SetNVENCConcurrency(maxConcurrentEncodes)` before any recorder starts, and seeds each recorder's `RecordCodec`/`RecordQuality`/`RecordFallbackCopy` (at-rest codec, quality, and stream-copy-on-failure flag — `FallbackToCopy == nil || *FallbackToCopy`, default on) from it.
- After configuring all recorders, if the storage codec re-encodes (`recording.ReEncodes`), warms the NVENC capability probe in the background (`go recording.StorageCodecUsable(...)`) so `GET /api/recording/storage/status` answers instantly on first request instead of running a throwaway ffmpeg encode inline.
- RTSP URI resolution order at startup: `cfg.StreamURL` override → ONVIF `SnapshotSource` fallback. `cfg.FallbackStreamUrl` is passed as `FallbackRTSPURI`.
- Passes the `recording.Manager` pointer to `VisionMonitorSettings.Recorder` so alert events automatically trigger clip extraction.
- Registers `recorderManager.Close()` in the graceful shutdown func.
- Builds a `services.PythonInstaller` (from `deps.DataDir`/`deps.ConfigPath`) and passes it into `NewSettingsApi` so Settings can install a self-contained AI Python runtime (Python + torch + ultralytics) in-app.
- Builds the `services.FFmpegInstaller` with `binDir = filepath.Abs(deps.DataDir/bin)` — a writable, absolute path — rather than a CWD-relative `bin`, mirroring the Python runtime under `dataDir/pyruntime`. A packaged Windows service runs with CWD `C:\Windows\System32`, so the old CWD-relative path misplaced the downloaded ffmpeg binary there instead of under the app's data directory.
- Builds a `services.UpdateService` (current version resolved from the embedded manifest via `versioning.LoadDefault()`/`InfoForApp`, `deps.HomeDir`, `deps.Restarter`), calls `CleanupStaleFiles()` at startup to remove any leftovers from a previous update, registers its periodic release check on `deps.Scheduler.StartPeriodic` when a scheduler is available, and passes it into `NewSystemApi` for the self-update check/apply endpoints.
- Builds a `services.NewBackupService` over the camera/camera-onvif/recording-config/detection-class/detection-rule/runtime-setting repositories plus `currentVersion`, and registers `apis.NewBackupApi` (after `NewSystemApi`) for the Settings → Backup & Recovery configuration backup/restore endpoints (`/settings/backup/*`).
- Provides API docs metadata and endpoint descriptions for shared Swagger/OpenAPI output.
- Uses the embedded app version as the OpenAPI info version when available.
- Registers `appentities.ObjectObservation{}` and `sharedentities.NotificationRollup{}` for bootstrap schema generation (the object-metadata recorder and the dashboard-analytics rollup table), plus two additional seeders: `mymatasan-recording-metadata-backfill` (backfills `recording_config.metadata_enabled`/`metadata_gap_seconds` to disabled defaults on existing rows, since a bare `ALTER TABLE` leaves them `NULL`) and `mymatasan-object-observation-indexes` (`CREATE INDEX IF NOT EXISTS` on `(camera_id, started_at)` and `(label, started_at)` — engine-portable secondary indexes the ORM's unique-index struct tags don't cover, needed for the observation search).
- Wires `apis.NewObservationApi` (object metadata search, `/api/observations`) and `apis.NewAnomalyApi` (statistical anomaly settings + on-demand scan, `/api/anomaly`) alongside the existing `NewNotificationApi`.

## Dashboard Intelligence & metadata recorder wiring

`RegisterAppRoutes` builds and wires the Dashboard Intelligence analytics suite (P0–P3) and the object metadata recorder, both sharing the monitor goroutine lifecycle (`monitorCtx`):

- **Metadata recorder (P0 dependency, ships with this batch):** `objectObservationRepo := dbsql.NewGenericRepo[appentities.ObjectObservation]`; `metadataRecorder := services.NewMetadataRecorder(objectObservationRepo, recordingService, deps.Config.Vision.Detector.MinObjectConfidence)` is the write side (fed object candidates via `vision.ObservationSink`, reusing the detector's inference — no second decode); `observationService := services.NewObservationService(objectObservationRepo, recordingService)` is the read/maintenance side (search + footage-segment linkage + purge). The detector is wired as the sink when it implements `vision.ObservationCapable` (`monitorSettings.Detector.(vision.ObservationCapable).SetObservationSink(metadataRecorder)`); `monitorSettings.Metadata = metadataRecorder` lets `VisionMonitor` sample metadata-enabled cameras even without alert rules. `metadataRecorder.Start(monitorCtx)` runs alongside `VisionMonitor.Start`, and `observationService.PurgeOldObservations` is added to the `mymatasan.purge.segments` periodic job (see "Retention purge jobs" below) alongside `recordingService.PurgeOldSegments` (retention aligned to each camera's recording retention, falling back to a 30-day default).
- **P0 — hourly rollup:** `notificationRollupRepo := dbsql.NewGenericRepo[sharedentities.NotificationRollup]`; `notificationService.WithRollups(notificationRollupRepo)` enables the analytics reads (Heatmap/Baseline/AnomalyScan); `notificationRollupMaintainer := notification.NewRollupMaintainer(notificationRepo, notificationRollupRepo, services.NewRollupCursor(runtimeSettingsRepo), 0, 0)` (default 60s interval / 5000-row page) incrementally folds the notifications table into `notification_rollup`, its watermark persisted via the `notification.rollup.cursor` runtime-setting key so a restart resumes instead of re-scanning history. `notificationRollupMaintainer.Start(monitorCtx)` — the first sweep (a few seconds after start) backfills all existing history on an upgrade.
- **P1/P2 — heatmap/baseline:** pure reads off the rollup, exposed via `GET /api/notifications/heatmap` and `GET /api/notifications/baseline` (no additional startup wiring beyond `WithRollups`).
- **P3 — statistical anomaly monitor:** `anomalySettingsService := services.NewAnomalySettingsService(runtimeSettingsRepo, services.DefaultAnomalySettings())` (persisted under the `anomalyDetection` runtime-setting key, opt-in/disabled by default); `services.NewAnalyticsMonitor(notificationService, notificationService, anomalySettingsService, cameraService).Start(monitorCtx)` scores the most recently closed hour against each camera's learned baseline on an interval and publishes `analytics.anomaly` category notifications for spikes/"unusual silence" (per-camera-per-direction debounce + cooldown). `apis.NewAnomalyApi(protected, anomalySettingsService, notificationService, cameraService)` exposes `GET/PUT /api/anomaly/settings` and `GET /api/anomaly/scan` (an on-demand preview scan for the Settings UI).
- **Reset gate:** `systemResetService` is declared as a `var` before `protected := api.PathPrefix("").Subrouter()` so `protected.Use(apis.NewResetGate(func() bool { return systemResetService != nil && systemResetService.InProgress() }))` can be registered — **before** the auth middleware — even though the real `SystemResetService` is constructed later in the function; the closure reads it live per request. This sheds load with a clean 503 instead of raw 500s while a reset has closed the DB pool and is still running the free-space scrub.
- **`CloseDatabase`:** `SystemResetConfig.CloseDatabase` is wired to `deps.Db.(io.Closer).Close()` when the configured `dbsql.IDbCrud` implementation exposes a `Close()` method (sqlite/mariadb/postgres all do now) — required on sqlite/Windows so the reset's database drop can actually delete the locked file.

## Retention purge jobs

A package-level `periodic(ctx, name, interval, fn)` helper runs `fn` once immediately, then on every `interval` tick, until `ctx` is done — wrapped in `infra/safego.Supervise`. The three purge loops that used to be near-identical copy-pasted `go func(){ ... ticker ... }()` blocks are now three `periodic(...)` calls sharing this one implementation:

- **`mymatasan.purge.segments`** (fixed 6h): `recordingService.PurgeOldSegments` and `observationService.PurgeOldObservations`. Their errors — previously discarded — are now logged via `deps.Logger.Warnf`.
- **`mymatasan.purge.notifications`** (`notification.purgeIntervalHours`, default 6h via `purgeInterval`): reads retention (days / onlyRead) live from notification settings each run, so UI changes take effect without a restart; calls `notificationService.PurgeOlderThanDays`.
- **`mymatasan.purge.alerts`** (`vision.alertPurgeIntervalHours`, default 6h via `purgeInterval`): purges the `alert_event` table. When `vision.diagnosticRetentionDays > 0`, calls `PurgeAlertsOlderThanDays(days, onlyDiagnostics=true)` to trim vision-monitor diagnostic rows without touching real detections; when `vision.alertRetentionDays > 0`, calls `PurgeAlertsOlderThanDays(days, onlyDiagnostics=false)` to also trim real detection alerts. Both paths unlink snapshot image files for removed rows.

Being supervised matters here specifically: a panic inside a purge used to kill the process, and simply recovering it would be no better, since nothing else notices a dead purge loop — the disk quietly fills (database writes included) until the next restart happens to bring it back.

## LPR model pointer

At startup, `lpr_model.txt` (alongside `active_model.txt` and `stock_model.txt` in the training dir) is resolved and its absolute path is written to `MYMATASAN_LPR_MODEL_FILE`. The YOLO worker reads this env var to know where to load the plate-detector weights. When the file is absent or empty, the LPR OCR stage never runs.

## Discovery responder

After the vision and health monitors are started, `app.go` conditionally starts the `infra/pairing` discovery responder (gated by `pairing.enabled`, default `true`). The responder:

- Runs under `infra/safego.Supervise` (name `mymatasan.pairing.responder`) rather than a bare `go`, so a panic restarts it with backoff instead of leaving the node silently un-adoptable (no further discovery probes would ever be answered) with nothing to say why.
- Reads the fleet key and discoverability live on every probe (`pairingService.FleetKey` / `pairingService.Discoverable`) so a key set or an adopt call takes effect without a restart.
- Goes silent automatically once the node is paired (because `Discoverable()` returns false).
- Advertises the first configured TLS port as `httpsPort` in announces so the control plane can build the adoption URL.
- Shares the `monitorCtx` lifecycle — it shuts down with the rest of the monitors on graceful shutdown.
- Logs diagnostics via the app logger under the `"mymatasan.pairing"` topic.

## Enrollment manager (mTLS)

Also within the monitor lifecycle, `app.go` builds and runs an `EnrollmentManager` (from `services/node_enrollment.go`):

- Built from `pairingService`, `pairing.mtlsPort` (default 49532), and `pairing.renewBeforeHours` (default 48h).
- `enrollmentManager.Kick` is passed as the `onAdopted` callback to `NewPairingPublicApi`, so enrollment begins immediately after the adopt call returns.
- `enrollmentManager.Run(monitorCtx)` is started as a goroutine; it reconciles on start, on `Kick`, and every 5 minutes.
- After adoption, it generates a key+CSR locally, POSTs to `<parentBaseURL>/api/nodes/enroll` for a signed certificate, and then serves a mutual-TLS management listener on `pairing.mtlsPort` (GET `/heartbeat`, POST `/release`).
- On unpair, the listener is torn down and the cert bundle is cleared.

## Media channel

Within the monitor lifecycle, `app.go` also builds and runs a `MediaChannelManager` (`services/media_channel.go`):

- Resolves each camera's RTSP source via `cameraService.SnapshotSource` (the same path used for browser live view); shares the `stream.Manager` RTSP session pool via the `MediaSubscriber` interface.
- Dials the parent's media listener (`pairing.mediaPort`, default 49534) over fleet mTLS.
- On a `FrameStart` from the parent, subscribes the requested camera and pumps live RTP (video + audio) up the channel.
- Reconnects with backoff (1 s → 30 s cap) when the channel drops; the parent re-sends `FrameStart` on reconnect.
- `mediaChannel.Run(monitorCtx)` is started as a goroutine alongside `controlChannel.Run`, sharing the monitor lifecycle.

## Notes

- Only the public shared version API is mounted for this standalone app.
- Shared login, user/group/role, app-registry, endpoint, endpoint-RBAC, file-storage, log, runtime-log, and cache-service route groups are disabled.
- App entity registration includes `OnvifDevice`, `RuntimeSetting`, `LocalUser`, `DetectionRule`, `AlertEvent`, `RecordingSegment`, `RecordingConfig`, `ObjectObservation`, `NotificationRollup`, and the pairing state rows stored in `RuntimeSetting` (no new table).
- The `/api/pairing` endpoint group is seeded with `Public` access tier because `adopt` and `release` carry their own cryptographic authentication.
- OpenAPI endpoint discovery is automatic; this module enriches summaries/descriptions via `APIDocs()`.
- Vision detector modes are `motion`, `external`, `hybrid`, and `persistent`; `persistent` keeps one detector worker process alive and closes it during app shutdown.
- At startup, per-camera recording configs with a missing RTSP URI are skipped with a warning log; recording starts only for cameras where an RTSP URI can be resolved.
- `PersistSampledDiagnostics` from `vision.persistSampledDiagnostics` is forwarded to `VisionMonitorSettings`; when false (default), the noisy per-frame heartbeat diagnostic is suppressed and only capture/detect failures are written.
- The default at-rest encryption key path (when `security.keyPath` is unset) resolves as `secret/atrest.key` against `deps.DataDir` via `apphost.ResolveWritablePath`, rather than a hardcoded CWD-relative path — upgrade-safe, since `ResolveWritablePath` keeps an existing legacy key (pre-packaging: CWD `secret/atrest.key`) in place so already-encrypted footage stays readable across an upgrade.

## Encryption-at-rest key resolution & recovery mode

`RegisterAppRoutes` resolves the master key **first, before building any other service** (moved from its former position mid-function):

- When `security.encryptAtRest` is on, it derives `keyPath` (as above) and `recoveryPath` (`security.recoveryPath`, default `recovery.atrestkey` beside the key) and builds `atrest.ProtectorConfig` from `security.keyProtector`/`passphrase`/`passphraseFile`/`passphraseEnv`, then calls `atrest.OpenForStartup(keyPath, recoveryPath, protectorCfg)`.
- On `ModeLoaded`/`ModeCreated`/`ModeRestored`, `atrestKeyStore`/`atrestCipher` are set as before and threaded into the recorder, vision monitor, and training image store; a `ModeRestored` outcome (key rebuilt from a config-driven recovery escrow) is logged distinctly.
- On `ModeRecoveryPending` (a key existed here before via the init marker but is now missing), `app.go` calls `apis.NewRecoveryGateApi(api, keyPath, protectorCfg, deps.Restarter, outcome.KeyId)` and **returns immediately** with a no-op shutdown func — no camera/vision/recording/API services are built, no DB writes happen, and nothing can mint a replacement key. The browser can reach nothing but the public recovery gate until the operator restores the key (see `apis/recovery_gate.go.md`) and the process restarts.
- `NewSystemApi` now takes a fifth `keystore` argument (the escrow-export/verify seam, `apis/system.go.md`); `app.go` passes `atrestKeyStore` as a narrow local interface value, or a typed-nil-free `nil` when encryption-at-rest is disabled, so the recovery endpoints cleanly report "not enabled" rather than panicking.
