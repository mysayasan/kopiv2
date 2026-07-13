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

- `type module struct` holds `cfg *mmconfig.Config` — mymatasan's own config (`camera`,
  `decoder`, `stream`, `vision`, `health`, `recording`), decoded from the same raw
  `config.json` document the host parsed, plus the health/camera monitors captured for
  `ReadinessStatus`.
- `(*module) DecodeAppConfig(raw []byte, dataDir string) error` implements
  `apphost.AppConfigDecoder` (`infra/apphost/types.go.md`): calls `mmconfig.Load(raw)`, then
  `cfg.Normalize(dataDir)` (resolves the recordings root and YOLO training dir against the
  writable data dir — code that used to live in `infra/apphost/run.go`), then stores the
  result on `m.cfg`. `infra/apphost/run.go` calls this once, after the shared config is
  loaded and normalized, before any route is registered; a returned error aborts startup.
  `m.cfg` is never `nil` inside `RegisterAppRoutes` as a result. See
  `docs/modules/apps/mymatasan/config/config.go.md` and `docs/MYMATASAN_TIER2_PLAN.md`
  (phase C).
- Provides app identity (`Name() = "mymatasan"`) and base directory.
- `(*module) Migrations() []bootstrap.Migration` implements `apphost.Migrator`
  (`infra/apphost/types.go.md`, Tier 2 phase M): returns `nil`, which is the normal state —
  an additive field change needs no migration, only a rename/drop/type-change/data-transform
  does. `infra/apphost/run.go` passes the result into the shared bootstrap `Options`. The
  factory-reset path in `RegisterAppRoutes` (below) passes the same `m.Migrations()` into its
  own `bootstrap.Options` too — omitting it there would leave a rebuilt database unable to
  baseline, and every migration would replay against a brand-new schema and fail. See
  `docs/modules/infra/db/bootstrap/migration.go.md`.
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
   `appCfg := m.cfg` is captured here too (mymatasan's own config, decoded earlier by
   `DecodeAppConfig`) — every step below that used to read `deps.Config.{Vision,Decoder,
   Stream,Health,Recording,Camera}` now reads the equivalent field off `appCfg`.
5. Constructs `cameraService`, `visionService`, `detectionClassService` and seeds built-in
   detection classes (`appCfg.Vision.Detector.ClassMap`).
6. `resolveDetectorModelPaths(deps, appCfg)` then `detectorPaths.PublishToProcessEnv()`, then
   `buildObjectDetectorBackend(deps, appCfg, detectorPaths)` (see `wire_vision.go.md`) —
   resolves the detector's worker-script path and model-pointer files into one typed value,
   publishes the pointers to the process environment for the Python worker, and builds the
   shared object-detection backend used by both the live monitor and the training
   auto-labeler.
7. Constructs `trainingService`, `settingsService`, `setupStateService`, `pairingService`,
   `localUserService`; resolves `shredPasses` (`config_map.go`, off `appCfg`); sizes the
   NVENC semaphore from the boot-time recording-storage settings; constructs
   `recordingService`, `metadataRecorder`, `observationService`, `notificationService` (+
   rollups/maintainer), and the settings services (notification/health/machine-health/
   anomaly). Syncs persisted notification delivery settings into the hub.
8. Seeds the first local admin user (or runs the one-shot `RESET_ADMIN` marker flow — see
   below) via `localUserService`.
9. Builds `streamManager`, `recorderManager`, wires the camera-delete cleanup cascade
   (`services.CameraDeletionCascade`, six ordered cleanups), builds `teachService`
   (via `teachDetectorConfig(appCfg, detectorPaths)`, `wire_vision.go`) and
   `recorderConfigBuilder`, then fans out `recorderManager.Configure` across every stored
   `RecordingConfig` in parallel goroutines (`sync.WaitGroup`). Warms the NVENC capability
   probe in the background when the boot-time codec re-encodes.
10. Builds `cameraHealthMonitor` / `machineHealthMonitor` (captured on `m` for
    `ReadinessStatus`) and `loginGuard`.
11. `buildFleet(...)` (see `wire_fleet.go.md`) — builds the three node-dialed fleet
    channels (enrollment/control/media) and registers the notification control-event sink.
12. Assembles the `wiring` struct `w` (see `wire_services.go.md`) — everything built so
    far including `appCfg`, gathered once so the remaining phases take one parameter instead
    of thirty. `w.validate()` is called immediately after and fails startup, naming the
    missing field, if anything (including `appCfg`) was left unset — see
    `wire_services.go.md`.
13. `registerRoutes(api, w)` (see `wire_routes.go.md`) — mounts the public routes, the
    middleware chain, and every protected API group; returns the protected subrouter.
14. Builds `resetMediaPaths` (a closure over `detectorPaths.TrainingDir` and `appCfg.Vision.
    SnapshotDir`), assembles `monitorSettings` inline via `visionMonitorSettingsFromAppConfig
    (appCfg)` + `wrapMonitorDetector(appCfg, objectBackend)` (it threads together the
    detector, recorder, notifier and metadata sink — none of which the pure `config_map.go`
    mapper can know about) and stores it on `w.visionMonitorSettings`.
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
- The 11 pure `*FromAppConfig` mappers (mymatasan's own `mmconfig.Config` — or, for the four
  mappers reading blocks that stayed shared, `config.AppConfigModel` — → service settings
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
  `mmconfig.VisionDetectorConfigModel` (external/hybrid/persistent modes; the type moved
  from `infra/config` to `apps/mymatasan/config` in Tier 2 phase C). Called from
  `wire_vision.go`'s `buildObjectDetectorBackend`.
- `wrapMonitorDetector(cfg *mmconfig.Config, backend)` — wraps the shared object backend
  into the live monitor's detector (rule mapping via `ObjectRuleDetector`, optional
  motion-intrusion dispatch); falls back to the native motion detector on a nil backend or
  `motion` mode. Called inline in `RegisterAppRoutes` when assembling `monitorSettings`.
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

## Tier 2 phase C: the per-app config seam

`m.cfg` (`*mmconfig.Config`, decoded by `DecodeAppConfig`) replaced `deps.Config.{Camera,
Decoder,Stream,Vision,Health,Recording}` throughout `RegisterAppRoutes` and every
`wire_*.go`/`config_map.go` function that read one of those six blocks. See
`docs/modules/apps/mymatasan/config/config.go.md` and `docs/MYMATASAN_TIER2_PLAN.md` (phase
C) for why (those blocks were mymatasan-only dead weight in the shared model, and
`infra/apphost` used to resolve their paths, giving the generic host hardcoded knowledge of
a vision feature) and for the "where the seam line is" judgement call (what moved vs. what
stayed shared).

This surfaced a real bug during the change, not just in theory: `wiring.appCfg` was left
unset on first cut, and the omission was a `nil`-pointer panic deep inside `registerRoutes`
at boot — nothing about the `wiring` struct's field list catches a forgotten field at
compile time. `wiring.validate()` (`wire_services.go.md`) is the fix: it fails startup
naming the missing field instead. Every wiring field gained the same check, not just
`appCfg`, since the same class of bug can recur for any of them.

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
