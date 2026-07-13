# Module: apps/mymatasan/app/wire_vision.go

## Purpose

The most consequential file in the D2 split. `detectorModelPaths` turns two previously
invisible dependencies — a config mutation and four bare `os.Setenv` calls — into one typed
value with explicit consumers and a single publication point.

## Responsibilities

- `type detectorModelPaths struct` — `TrainingDir`, `ActiveModelFile`, `StockModelFile`,
  `LPRModelFile`, `AnomalyManifestFile` (all absolute paths under the training dir) and
  `DetectorArgs` (the worker-script arguments, resolved to absolute paths against
  `deps.HomeDir` via `resolveDetectorScriptArgs` in `app.go`).
- `resolveDetectorModelPaths(deps apphost.Dependencies) detectorModelPaths` — resolves
  `trainingDataDir(deps.Config)` and every pointer-file path under it, plus the resolved
  detector args. The script resolution is against `deps.HomeDir` so it's found regardless
  of process working directory: a dev run from the repo root, or the staged `bin/` bundle
  (where the script sits in `<HomeDir>/ai` and a repo-root-relative config path would
  otherwise double up as `<bin>/apps/mymatasan/ai/...`).
- `(p detectorModelPaths) PublishToProcessEnv()` — the **one** place that calls
  `os.Setenv`, exporting all four model-pointer paths
  (`MYMATASAN_ACTIVE_MODEL_FILE`/`_STOCK_MODEL_FILE`/`_LPR_MODEL_FILE`/`_ANOMALY_FILE`)
  into the process environment, where the Python YOLO worker reads them. Called once, from
  `RegisterAppRoutes`, immediately after `resolveDetectorModelPaths`.
- `(p detectorModelPaths) Env() []string` — renders the same four pointers as `KEY=VALUE`
  pairs, for a spawn site that wants to hand a child process an explicit environment
  instead of relying on inheritance (`infra/vision`'s `PersistentOptions.Env` supports
  this).
- `detectorConfigWithArgs(cfg config.VisionDetectorConfigModel, args []string)
  config.VisionDetectorConfigModel` — returns a **copy** of the detector config with
  `Args` replaced by the resolved ones. Takes a copy deliberately: the shared config model
  must never be mutated, because a later reader has no way to know whether the write has
  happened yet.
- `buildObjectDetectorBackend(deps, paths detectorModelPaths) vision.ObjectDetector` —
  builds the shared object-detection backend (via `buildTrainingObjectDetector` in
  `app.go`, using `detectorConfigWithArgs`) used by both the live monitor and the training
  auto-labeler. A build failure is not fatal: logs a warning and returns `nil`, which
  disables auto-label and custom models.
- `teachDetectorConfig(deps, paths detectorModelPaths) services.TeachDetectorConfig` —
  builds the Teach wizard's detector settings from the resolved paths (command, resolved
  args, timeout, anomaly manifest path), so Teach cannot silently pick up unresolved
  script arguments.

## Notes

- **Fixes ordering hazard #1** (see `docs/modules/apps/mymatasan/app/app.go.md`): the
  detector's script arguments used to be resolved by mutating `deps.Config` in place, and
  three later constructors (training run config, vision-tool settings, Teach detector
  config) silently depended on that write having already happened. Move one line and
  training resolves the wrong worker script — no compile error, no failing test. Now the
  resolved args are a value every consumer takes as a parameter.
- **The env channel still exists deliberately.** Several Python spawn sites — the vision
  tool, the Teach anomaly checker, the training runner — inherit the process environment
  rather than being handed the paths directly. Removing `PublishToProcessEnv` entirely
  means threading `detectorModelPaths` (or its `Env()` output) into each of those spawn
  sites individually. That is a follow-up, Tier 2 **phase D3**
  (`docs/MYMATASAN_TIER2_PLAN.md`), not something to fold into a mechanical decomposition.
  What changed here is that the four bare `os.Setenv` calls in the middle of `app.go`'s
  wiring became one type with one call site.
- Pure move + the type-safety change above; no behavior change for a correctly configured
  install — the same four env vars are set to the same values, at the same relative point
  in startup, as before.
