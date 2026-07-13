# Module: apps/mymatasan/app/app.go

## Purpose

Implements the `mymatasan` app module for the shared runtime host. `RegisterAppRoutes` is
now the **composition-root sequencer**: it calls one builder function per subsystem, each
living in its own `wire_*.go` file (Tier 2 phase D2), and threads their outputs together.
Before this split the function was 792 lines with 14 responsibilities and several
comment-enforced ordering contracts; it is now ~490 lines that mostly just call, in order:
`openAtRest` → `newRepos` → `resolveDetectorModelPaths`/`buildObjectDetectorBackend` →
(inline service construction) → `buildFleet` → `registerRoutes` → (inline monitor-settings
assembly) → `startBackgroundWorkers`. See the sibling `wire_*.go.md` docs for what each
phase does; this doc covers what's left in `app.go` itself: the module manifest, the
sequencing, and the helpers that don't belong to any one subsystem.

## Responsibilities (module manifest)

- Provides app identity (`Name() = "mymatasan"`) and base directory.
- `ReadinessStatus` contributes machine and camera health (captured on the `module` struct
  during `RegisterAppRoutes`) to the shared `/ready` payload — advisory only, never flips
  the ready/not-ready verdict.
- Registers app entities for bootstrap schema generation, including
  `appentities.ObjectObservation{}` and `sharedentities.NotificationRollup{}` (object
  metadata recorder + dashboard-analytics rollup table).
- Registers built-in and config-driven seeders: RBAC endpoint metadata, the
  `is_diagnostic`/camera-health/`recording_config` metadata NULL-backfills for columns
  added via `ALTER TABLE`, and `CREATE INDEX IF NOT EXISTS` secondary indexes for the
  object-observation search.
- `APIDocs()` provides API docs metadata and endpoint descriptions for shared
  Swagger/OpenAPI output, using the embedded app version as the OpenAPI info version when
  available.

## `RegisterAppRoutes` — the sequence

1. `safego.SetLogger(...)` — routes recovered background-goroutine panics into the app
   logger before anything starts, so a panic is never lost to stdout on a service install.
2. `services.DescribeMetrics` / `recording.DescribeMetrics` — registers `/metrics` help
   text before anything can observe those metrics.
3. `openAtRest(api, deps)` (see `wire_security.go.md`) — resolves the encryption-at-rest
   master key **first, before any other service is built**. If it returns
   `RecoveryPending`, `RegisterAppRoutes` mounts nothing else and returns a no-op shutdown
   func immediately — the recovery gate API was already mounted inside `openAtRest`.
4. `newRepos(deps.Db)` (see `wire_storage.go.md`) — builds all 16 repositories in one call.
5. Constructs `cameraService`, `visionService`, `detectionClassService` and seeds built-in
   detection classes.
6. `resolveDetectorModelPaths(deps)` then `detectorPaths.PublishToProcessEnv()`, then
   `buildObjectDetectorBackend(deps, detectorPaths)` (see `wire_vision.go.md`) — resolves
   the detector's worker-script path and model-pointer files into one typed value, publishes
   the pointers to the process environment for the Python worker, and builds the shared
   object-detection backend used by both the live monitor and the training auto-labeler.
7. Constructs `trainingService`, `settingsService`, `setupStateService`, `pairingService`,
   `localUserService`; resolves `shredPasses` (`config_map.go`); sizes the NVENC semaphore
   from the boot-time recording-storage settings; constructs `recordingService`,
   `metadataRecorder`, `observationService`, `notificationService` (+ rollups/maintainer),
   and the settings services (notification/health/machine-health/anomaly). Syncs persisted
   notification delivery settings into the hub.
8. Seeds the first local admin user (or runs the one-shot `RESET_ADMIN` marker flow — see
   below) via `localUserService`.
9. Builds `streamManager`, `recorderManager`, wires the camera-delete cleanup cascade
   (`services.CameraDeletionCascade`, six ordered cleanups), builds `teachService`
   (via `teachDetectorConfig(deps, detectorPaths)`, `wire_vision.go`) and
   `recorderConfigBuilder`, then fans out `recorderManager.Configure` across every stored
   `RecordingConfig` in parallel goroutines (`sync.WaitGroup`). Warms the NVENC capability
   probe in the background when the boot-time codec re-encodes.
10. Builds `cameraHealthMonitor` / `machineHealthMonitor` (captured on `m` for
    `ReadinessStatus`) and `loginGuard`.
11. `buildFleet(...)` (see `wire_fleet.go.md`) — builds the three node-dialed fleet
    channels (enrollment/control/media) and registers the notification control-event sink.
12. Assembles the `wiring` struct `w` (see `wire_services.go.md`) — everything built so
    far, gathered once so the remaining phases take one parameter instead of thirty.
13. `registerRoutes(api, w)` (see `wire_routes.go.md`) — mounts the public routes, the
    middleware chain, and every protected API group; returns the protected subrouter.
14. Builds `resetMediaPaths` (a closure over `detectorPaths.TrainingDir` and friends),
    assembles `monitorSettings` inline (it threads together the detector, recorder,
    notifier and metadata sink — none of which the pure `config_map.go` mapper can know
    about) and stores it on `w.visionMonitorSettings`.
15. `startBackgroundWorkers(monitorCtx, w)` (see `wire_monitors.go.md`) — starts every
    long-lived worker: the vision monitor + warmup, health monitors, rollup maintainer,
    analytics monitor, the discovery responder + fleet channels (all now supervised), and
    the retention purge loops.
16. Builds `systemResetService` (needs the monitors/recorder to exist so its
    `StopServices` hook can quiesce them before a wipe) and publishes it onto
    `w.systemReset` — this is what arms the `ResetGate` middleware that was registered
    back in step 13, which reads it through a closure at request time.
17. Builds `updateService` (self-update check/apply) and `backupService`
    (`apis.NewBackupApi`, reusing the repos from `newRepos`).
18. Returns the graceful-shutdown func: stops monitors, closes the recorder manager,
    closes the notification service, closes the detector/training service if they
    implement `io.Closer`, closes the stream manager.

## What moved out (see the sibling docs)

- Encryption-at-rest key resolution + recovery mode → `wire_security.go.md`.
- The 16 repository constructions → `wire_storage.go.md`.
- Detector worker-script/model-pointer resolution, the `os.Setenv` publication, and the
  shared object-detection backend build → `wire_vision.go.md`.
- The three node-dialed fleet channels → `wire_fleet.go.md`.
- The middleware chain and every protected API-group registration → `wire_routes.go.md`.
- Starting every background worker and the three retention purge loops →
  `wire_monitors.go.md`.
- The `wiring` struct that threads everything between phases → `wire_services.go.md`.
- The 11 pure `*FromAppConfig` mappers (`config.AppConfigModel` → service settings
  structs) → `config_map.go.md`.

## What's still here

- First-run admin credential flow: seeds the default admin via
  `localUserService.EnsureDefaultAdmin`; when `Seeded`, `announceFirstRunAdmin` writes
  `INITIAL_ADMIN_LOGIN.txt` (0600) to the data dir via `writeFirstRunCredentialFile` and
  prints a console sign-in banner (URL via `firstRunConsoleURL`). The password is echoed
  only when generated; a config/env-supplied one is pointed at, not logged.
- One-shot admin reset: checks for the `RESET_ADMIN` marker (`adminResetMarkerFile`,
  `fileExists`) dropped by the Windows installer's "reset admin login" option, deletes the
  marker before acting (so a restart never re-runs it), then calls
  `localUserService.ResetAdmin` and `announceFirstRunAdmin`.
- `resolveDetectorScriptArgs`/`resolveDetectorScript`/`isRegularFile` — resolves the
  detector's Python worker-script argument to an absolute path against `deps.HomeDir`.
  Called from `wire_vision.go`'s `resolveDetectorModelPaths`, but stays in `app.go`
  because it's a general path-resolution helper, not vision-specific wiring per se.
- `buildTrainingObjectDetector` — builds the raw `vision.ObjectDetector` backend from
  `config.VisionDetectorConfigModel` (external/hybrid/persistent modes). Called from
  `wire_vision.go`'s `buildObjectDetectorBackend`.
- `wrapMonitorDetector` — wraps the shared object backend into the live monitor's detector
  (rule mapping via `ObjectRuleDetector`, optional motion-intrusion dispatch); falls back
  to the native motion detector on a nil backend or `motion` mode. Called inline in
  `RegisterAppRoutes` when assembling `monitorSettings`.
- `periodic(ctx, name, interval, fn)` — runs `fn` once immediately then on every interval
  tick under `safego.Supervise`, until `ctx` is done. Used by `wire_monitors.go`'s
  `startRetentionPurges` for the three purge loops.
- `appVersion(appName)` — resolves this app's version from the embedded version manifest
  (best-effort, empty on failure). Used for the control-channel Hello and `APIDocs()`.

## Notes

- Only the public shared version API is mounted for this standalone app.
- Shared login, user/group/role, app-registry, endpoint, endpoint-RBAC, file-storage, log,
  runtime-log, and cache-service route groups are disabled.
- The `/api/pairing` endpoint group is seeded with `Public` access tier because `adopt` and
  `release` carry their own cryptographic authentication.
- OpenAPI endpoint discovery is automatic; this module enriches summaries/descriptions via
  `APIDocs()`.
- **No behavior change from this split**: every call, ordering, and setting is the same as
  before — this is a pure decomposition. The two things that *did* change are the two
  ordering hazards below, and they were bug fixes, not behavior changes for a correctly
  configured install.

## The two ordering hazards, now type-enforced

1. **`deps.Config` is no longer mutated.** Previously `deps.Config.Vision.Detector.Args`
   was overwritten in place with the resolved script path, and three later constructors
   (`trainingRunConfigFromAppConfig`, `visionToolSettingsFromAppConfig`,
   `services.TeachDetectorConfig`) silently depended on that write having already
   happened — move one line and training would resolve the wrong worker script, with no
   compile error and no failing test. The resolved args are now a value
   (`detectorModelPaths.DetectorArgs`, see `wire_vision.go.md`) that every consumer takes
   as an explicit parameter: `trainingRunConfigFromAppConfig(cfg, configPath,
   detectorArgs)` and `visionToolSettingsFromAppConfig(cfg, detectorArgs)` both gained a
   third/second argument (`config_map.go.md`).
2. **`os.Setenv` no longer appears in `app.go`.** The four bare `os.Setenv` calls
   (`MYMATASAN_ACTIVE_MODEL_FILE`/`_STOCK_`/`_LPR_`/`_ANOMALY_FILE`) — the inter-component
   channel to the Python YOLO worker — are now one typed value
   (`detectorModelPaths`) with one publication point (`PublishToProcessEnv()`, called once
   from `RegisterAppRoutes`). The env channel still exists (`wire_vision.go.md` explains
   why: several Python spawn sites inherit the process environment rather than being
   handed the paths directly) — removing it entirely is Tier 2 phase D3
   (`docs/MYMATASAN_TIER2_PLAN.md`).

## Latent bug fix: fleet goroutines now supervised

The three fleet loops (`enrollmentManager.Run`, `controlChannel.Run`,
`mediaChannel.Run`) were bare `go` calls. A panic in any of them took the whole process
down, and their death was otherwise silent — the node would simply stop enrolling, stop
answering the parent, or stop relaying live video, with nothing in the logs to say why. All
three are now started via `safego.Supervise` in `wire_monitors.go`'s
`startBackgroundWorkers`, alongside the pairing discovery responder (which was already
supervised). See `docs/TECHNICAL_SPEC.md`'s background-goroutine-resilience section.

## Encryption-at-rest key resolution & recovery mode

Delegated to `openAtRest` in `wire_security.go` (see `wire_security.go.md` for the full
behavior); `RegisterAppRoutes` still calls it first, before building any other service, and
still returns immediately with a no-op shutdown func on `RecoveryPending`.
