# Module: apps/mymatasan/app/app.go

## Purpose

Implements the `mymatasan` app module for the shared runtime host.

## Responsibilities

- Provides app identity and base directory.
- Registers app entities for bootstrap schema generation.
- Registers built-in and config-driven seeders.
- Wires app-specific APIs (`onvif`, `settings`, `vision`, `recording`).
- Mounts app-specific APIs behind standalone DB-backed local Basic Auth.
- Seeds the first local admin user when no local users exist, via `localUserService.EnsureDefaultAdmin(ctx, deps.Config.LocalAuth.Username, deps.Config.LocalAuth.Password)` (generates a per-install password when config/env supply none; always flags the seeded account must-change). When the result reports `Seeded`, `announceFirstRunAdmin` reveals the bootstrap login on the non-Windows install paths (CLI/Docker/systemd/portable, which have no GUI installer finish page): it prints a console sign-in banner (URL via `firstRunConsoleURL` — https on a TLS port else http, first configured port, default 3000; username; and the generated password) and, when the password was generated, writes an `INITIAL_ADMIN_LOGIN.txt` recovery file (0600) to the data dir via `writeFirstRunCredentialFile`. A config/env-supplied password (`Generated=false`) is neither echoed nor written.
- Owns the app-local stream manager used by WebRTC live view and closes it during graceful shutdown.
- Wires SQLite-backed runtime settings seeded from `decoder` and `stream` config defaults.
- Builds the app-local vision detector from `vision.detector` config and starts the monitor worker when `vision.enabled` allows it.
- Resolves the detector's Python worker script (`vision.detector.args`) to an absolute path against `deps.HomeDir` via `resolveDetectorScriptArgs`/`resolveDetectorScript`, so the worker is found regardless of the process working directory: a dev run from the repo root (`HomeDir=apps/mymatasan`) or the staged `bin/` bundle (`HomeDir=<exe dir>`, with `ai/` staged alongside). The default config uses the HomeDir-relative `ai/yolo_worker.py`; legacy repo-root-relative values (`./apps/mymatasan/ai/yolo_worker.py`) are recovered by basename in `<HomeDir>/ai` (they otherwise doubled to `<bin>/apps/mymatasan/ai/...` and the worker failed to open, killing detection/calibration). The training script + base-model paths are derived from the resolved worker directory, so they follow automatically.
- Initialises the `recording.Manager` and applies all enabled `RecordingConfig` rows at startup via `Manager.Configure`.
- Reads the runtime `recording.storage` settings at startup: sizes the shared NVENC semaphore via `recording.SetNVENCConcurrency(maxConcurrentEncodes)` before any recorder starts, and seeds each recorder's `RecordCodec`/`RecordQuality` (at-rest codec) from it.
- RTSP URI resolution order at startup: `cfg.StreamURL` override → ONVIF `SnapshotSource` fallback. `cfg.FallbackStreamUrl` is passed as `FallbackRTSPURI`.
- Passes the `recording.Manager` pointer to `VisionMonitorSettings.Recorder` so alert events automatically trigger clip extraction.
- Registers `recorderManager.Close()` in the graceful shutdown func.
- Builds a `services.PythonInstaller` (from `deps.DataDir`/`deps.ConfigPath`) and passes it into `NewSettingsApi` so Settings can install a self-contained AI Python runtime (Python + torch + ultralytics) in-app.
- Builds the `services.FFmpegInstaller` with `binDir = filepath.Abs(deps.DataDir/bin)` — a writable, absolute path — rather than a CWD-relative `bin`, mirroring the Python runtime under `dataDir/pyruntime`. A packaged Windows service runs with CWD `C:\Windows\System32`, so the old CWD-relative path misplaced the downloaded ffmpeg binary there instead of under the app's data directory.
- Builds a `services.UpdateService` (current version resolved from the embedded manifest via `versioning.LoadDefault()`/`InfoForApp`, `deps.HomeDir`, `deps.Restarter`), calls `CleanupStaleFiles()` at startup to remove any leftovers from a previous update, registers its periodic release check on `deps.Scheduler.StartPeriodic` when a scheduler is available, and passes it into `NewSystemApi` for the self-update check/apply endpoints.
- Builds a `services.NewBackupService` over the camera/camera-onvif/recording-config/detection-class/detection-rule/runtime-setting repositories plus `currentVersion`, and registers `apis.NewBackupApi` (after `NewSystemApi`) for the Settings → Backup & Recovery configuration backup/restore endpoints (`/settings/backup/*`).
- Provides API docs metadata and endpoint descriptions for shared Swagger/OpenAPI output.
- Uses the embedded app version as the OpenAPI info version when available.

## Alert-log retention background job

A goroutine started at startup periodically purges the `alert_event` table (default every 6 hours, configurable via `vision.alertPurgeIntervalHours`). It runs once immediately at startup, then on the ticker:
- When `vision.diagnosticRetentionDays > 0`, calls `PurgeAlertsOlderThanDays(days, onlyDiagnostics=true)` to trim Vision-monitor diagnostic rows without touching real detections.
- When `vision.alertRetentionDays > 0`, calls `PurgeAlertsOlderThanDays(days, onlyDiagnostics=false)` to also trim real detection alerts.
Both paths unlink snapshot image files for removed rows.

## LPR model pointer

At startup, `lpr_model.txt` (alongside `active_model.txt` and `stock_model.txt` in the training dir) is resolved and its absolute path is written to `MYMATASAN_LPR_MODEL_FILE`. The YOLO worker reads this env var to know where to load the plate-detector weights. When the file is absent or empty, the LPR OCR stage never runs.

## Discovery responder

After the vision and health monitors are started, `app.go` conditionally starts the `infra/pairing` discovery responder (gated by `pairing.enabled`, default `true`). The responder:

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
- App entity registration includes `OnvifDevice`, `RuntimeSetting`, `LocalUser`, `DetectionRule`, `AlertEvent`, `RecordingSegment`, `RecordingConfig`, and the pairing state rows stored in `RuntimeSetting` (no new table).
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
