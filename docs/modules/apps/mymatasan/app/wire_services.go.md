# Module: apps/mymatasan/app/wire_services.go

## Purpose

`wiring` is a plain struct holding everything the composition root has built: the
services, the managers, and the resolved values they were built from. Introduced in Tier 2
phase D2 so `registerRoutes` and `startBackgroundWorkers` take ONE parameter instead of
thirty free variables pulled out of an 800-line scope.

## Responsibilities

- `type wiring struct` — grouped fields:
  - `deps apphost.Dependencies` — the original app-host dependencies.
  - `appCfg *mmconfig.Config` — mymatasan's own config (`camera`/`decoder`/`stream`/
    `vision`/`health`/`recording`), decoded from the same `config.json` document
    `deps.Config` was decoded from. Added in Tier 2 phase C (the per-app config seam,
    `docs/MYMATASAN_TIER2_PLAN.md`); see `docs/modules/apps/mymatasan/config/config.go.md`.
  - Resolved values: `atrestKeyStore`, `atrestCipher`, `detectorPaths` (`detectorModelPaths`,
    see `wire_vision.go.md`), `objectBackend`, `httpsPort`.
  - Domain services: `camera`, `vision`, `detectionClass`, `training`, `teach`,
    `recording`, `observation`, `metadata`, `localUser`, `setupState`, `pairing`.
  - Settings services: `settings`, `notificationSettings`, `healthSettings`,
    `machineHealthSettings`, `anomalySettings`.
  - Managers and monitors: `notification`, `notificationRollup`, `recorder`,
    `recorderConfig`, `streamManager`, `cameraHealth`, `machineHealth`,
    `visionMonitorSettings`.
  - Fleet: `enrollment`, `control`, `media` (see `wire_fleet.go.md`).
  - Auth: `loginGuard`, `loginLockoutNotifier`.
  - Installers: `ffmpegInstaller`, `pythonInstaller`.
  - `systemReset *services.SystemResetService` — built LAST in `RegisterAppRoutes` (it
    needs the monitors and recorder to exist so it can quiesce them before wiping). The
    reset-gate middleware is registered before this field is set and reads it through a
    closure at request time, so `w.systemReset = systemResetService` is what arms the gate.
- `(*wiring) validate() error` — fails fast, naming every missing field, if a pointer/struct
  field on `wiring` was left unset (nil) when the composition root finished populating it.
  Checks every field except `systemReset` (legitimately nil at the point `validate` runs;
  set later) and `deps`/`httpsPort` (not pointers). Called from `app.go`'s
  `RegisterAppRoutes` immediately after the `wiring` struct literal is built, before
  `registerRoutes(api, w)`.

## Notes

- Deliberately **not** a service locator: nothing resolves anything from it at runtime.
  The composition root fills it once, in order, in `app.go`, and the phase functions
  (`registerRoutes`, `startBackgroundWorkers`) just read the fields they need. If a phase
  needs something new, it has to be added to this struct — that requirement is the point:
  the struct is the visible seam of what each phase depends on.
- `ffmpegBinDir`/`ffmpegInstaller`/`pythonInstaller` construction moved from being inline
  in `app.go`'s route-registration block into the `wiring` struct literal build, as part
  of this same decomposition.
- Pure move/aggregation from `app.go`; the values it carries and how they're built are
  unchanged.
- **`validate()` exists because of a real bug, not a hypothetical one.** Gathering
  everything into one struct means a forgotten field is a `nil`-pointer dereference deep
  inside a phase at runtime rather than a compile error — and that's exactly what happened
  when the config seam (Tier 2 phase C) shipped: `appCfg` was left unset in the struct
  literal and the process panicked in `registerRoutes` on boot, with a stack trace that
  didn't say which field was missing. `validate()` turns that into a startup error naming
  the field. Every field on the struct got the same check as a result, not just `appCfg`,
  since the same class of omission can recur for any of them as the struct grows.
