# Module: apps/mymatasan/app/config_map.go

## Purpose

Holds the 11 pure `*FromAppConfig` mappers that translate config blocks into each service's
settings struct, moved out of `app.go` (Tier 2 phase D2) so the composition root reads as
sequencing, not struct-copying.

Since Tier 2 phase C (the per-app config seam), these mappers split by which model owns
their source block: 7 take mymatasan's own `*mmconfig.Config`
(`apps/mymatasan/config`, imported `mmconfig`), 4 still take the shared
`*config.AppConfigModel` (`infra/config`) because their block (`loginSecurity`,
`notification`) stayed shared. See `docs/modules/apps/mymatasan/config/config.go.md` for
which blocks moved.

## Responsibilities

- `loginGuardConfigFromAppConfig(cfg *config.AppConfigModel)` — maps `loginSecurity` into
  `apis.LoginGuardConfig`. **Shared model** — `loginSecurity` did not move.
- `notificationOptionsFromAppConfig(cfg *config.AppConfigModel, logger, metrics)` — builds
  always-on `notification.Options` (logger, SSE client buffer, metrics). Outbound delivery
  channels (webhook/telegram) are applied separately, from the persisted runtime-editable
  settings. **Shared model** — `notification` did not move.
- `notificationSettingsDefaultsFromAppConfig(cfg *config.AppConfigModel)` — maps
  `notification` into `services.NotificationSettings`, seeding the persisted settings on
  first run only. **Shared model.**
- `runtimeSettingsFromAppConfig(cfg *mmconfig.Config)` — maps `decoder`/`stream`/`recording`
  into `services.RuntimeSettings`. **App-owned model.**
- `visionMonitorSettingsFromAppConfig(cfg *mmconfig.Config)` — builds
  `services.VisionMonitorSettings` **without** the detector; the caller (`app.go`) assigns
  `Detector` separately via `wrapMonitorDetector` off the shared object backend, so the same
  backend serves live detection and auto-label. **App-owned model.**
- `healthSettingsDefaultsFromAppConfig(cfg *mmconfig.Config)` — maps `health` into
  `services.HealthSettings`, seeding persisted settings on first run only. **App-owned
  model.**
- `resolveShredPasses(cfg *mmconfig.Config)` — decides secure-overwrite pass count for
  deleted footage: 0 when `recording.shred.enabled=false`, else `recording.shred.passes`
  when positive, else `recording.DefaultShredPasses`. **App-owned model.**
- `trainingDataDir(cfg *mmconfig.Config)` — resolves the on-disk training root:
  `vision.training.dataDir` if set, else a `training` sibling of the snapshot dir.
  **App-owned model.**
- `trainingRunConfigFromAppConfig(cfg *mmconfig.Config, configPath, detectorArgs)
  services.TrainingRunConfig` — derives the Python trainer command and
  `train_worker.py`/base-weights paths that sit next to the configured YOLO worker script.
  **`detectorArgs` is a parameter, not read from `cfg.Vision.Detector.Args`** — see Notes.
  **App-owned model.**
- `visionToolSettingsFromAppConfig(cfg *mmconfig.Config, detectorArgs)
  services.VisionToolSettings` — builds the on-demand vision tool's settings. Same
  `detectorArgs`-as-parameter reasoning. **App-owned model.**
- `boolValue(value *bool, fallback bool) bool` — nil-safe bool-pointer dereference used by
  most of the above and by several `wire_*.go` files. Model-agnostic.

## Notes

- Most of this file exists only because config blocks have to be hand-copied into a
  parallel service-settings struct rather than the service reading the config model
  directly. Tier 2 phase C (the per-app config seam, `docs/MYMATASAN_TIER2_PLAN.md`) moved
  the app-owned half of that copying (`Vision`, `Recording`, `Decoder`, `Stream`, `Health`)
  onto mymatasan's own model, which is why 7 of the 11 mappers here now take `*mmconfig.Config`
  instead of `*config.AppConfigModel`; it did not collapse the copying itself — that would
  be a further step, not attempted here.
- `trainingRunConfigFromAppConfig` and `visionToolSettingsFromAppConfig` both changed
  signature in the D2 refactor (independent of the phase-C model-type change above): they
  used to read `cfg.Vision.Detector.Args` directly, which only worked because `app.go` had
  mutated the shared config model in place a few lines earlier — an ordering contract
  enforced by a comment. They now take the **resolved** (absolute-path) detector args as an
  explicit parameter (`detectorModelPaths.DetectorArgs`, see `wire_vision.go.md`), so the
  compiler enforces the ordering instead of a comment. All functions in this file remain
  pure (no I/O, no dependency on wiring order) and are individually unit-testable.
