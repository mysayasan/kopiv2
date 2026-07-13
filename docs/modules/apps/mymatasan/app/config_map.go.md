# Module: apps/mymatasan/app/config_map.go

## Purpose

Holds the 11 pure `*FromAppConfig` mappers that translate `config.AppConfigModel` blocks
into each service's settings struct, moved out of `app.go` (Tier 2 phase D2) so the
composition root reads as sequencing, not struct-copying.

## Responsibilities

- `loginGuardConfigFromAppConfig(cfg)` — maps `loginSecurity` into `apis.LoginGuardConfig`.
- `notificationOptionsFromAppConfig(cfg, logger, metrics)` — builds always-on
  `notification.Options` (logger, SSE client buffer, metrics). Outbound delivery channels
  (webhook/telegram) are applied separately, from the persisted runtime-editable settings.
- `notificationSettingsDefaultsFromAppConfig(cfg)` — maps `notification` into
  `services.NotificationSettings`, seeding the persisted settings on first run only.
- `runtimeSettingsFromAppConfig(cfg)` — maps `decoder`/`stream`/`recording` into
  `services.RuntimeSettings`.
- `visionMonitorSettingsFromAppConfig(cfg)` — builds `services.VisionMonitorSettings`
  **without** the detector; the caller (`app.go`) assigns `Detector` separately via
  `wrapMonitorDetector` off the shared object backend, so the same backend serves live
  detection and auto-label.
- `healthSettingsDefaultsFromAppConfig(cfg)` — maps `health` into
  `services.HealthSettings`, seeding persisted settings on first run only.
- `resolveShredPasses(cfg)` — decides secure-overwrite pass count for deleted footage:
  0 when `recording.shred.enabled=false`, else `recording.shred.passes` when positive,
  else `recording.DefaultShredPasses`.
- `trainingDataDir(cfg)` — resolves the on-disk training root: `vision.training.dataDir`
  if set, else a `training` sibling of the snapshot dir.
- `trainingRunConfigFromAppConfig(cfg, configPath, detectorArgs) services.TrainingRunConfig`
  — derives the Python trainer command and `train_worker.py`/base-weights paths that sit
  next to the configured YOLO worker script. **`detectorArgs` is a parameter, not read
  from `cfg.Vision.Detector.Args`** — see Notes.
- `visionToolSettingsFromAppConfig(cfg, detectorArgs) services.VisionToolSettings` — builds
  the on-demand vision tool's settings. Same `detectorArgs`-as-parameter reasoning.
- `boolValue(value *bool, fallback bool) bool` — nil-safe bool-pointer dereference used by
  most of the above and by several `wire_*.go` files.

## Notes

- Most of this file exists only because the shared `config.AppConfigModel` carries
  mymatasan's blocks (`Vision`, `Recording`, `Decoder`, `Stream`, `Health`,
  `Notification`) and each has to be hand-copied into a parallel service-settings struct.
  Tier 2 phase C (the per-app config seam, `docs/MYMATASAN_TIER2_PLAN.md`) collapses that
  triplication.
- `trainingRunConfigFromAppConfig` and `visionToolSettingsFromAppConfig` both changed
  signature in this refactor: they used to read `cfg.Vision.Detector.Args` directly, which
  only worked because `app.go` had mutated the shared config model in place a few lines
  earlier — an ordering contract enforced by a comment. They now take the **resolved**
  (absolute-path) detector args as an explicit parameter
  (`detectorModelPaths.DetectorArgs`, see `wire_vision.go.md`), so the compiler enforces
  the ordering instead of a comment. All functions in this file remain pure (no I/O, no
  dependency on wiring order) and are individually unit-testable.
