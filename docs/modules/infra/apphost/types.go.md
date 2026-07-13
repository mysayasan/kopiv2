# Module: infra/apphost/types.go

## Purpose

Defines the app module contract used by the shared runtime host.

## Key Types

- `App`
  - app identity (`Name`, `BaseDir`)
  - entity registry (`Entities`)
  - seeder registry (`Seeders`)
  - app-specific route registration (`RegisterAppRoutes`)
- `Dependencies`
  - shared runtime dependencies passed into app modules
  - includes database, cache, `Auth` (JWT middleware), `Access` (shared `AccessSessionMidware` — the accessrbac enforcement middleware), `AccessRoles` (`IAccessRoleService`), `AccessPerms` (`IAccessPermissionService`), app registry, logger, scheduler, `Metrics` (`telemetry.Metrics`, never nil — a no-op recorder when telemetry is disabled), and `Restarter`
  - `HomeDir` — the read-only application root (static assets, bundled scripts, the default config); `DataDir` — the writable state root (config, database, recordings, logs, keys). Equal in a source/dev checkout; a packaged install sets them apart via `<APP>_HOME`/`<APP>_DATA` (or `KOPIV2_HOME`/`KOPIV2_DATA`) env vars. Apps resolve their own writable paths against `DataDir` (see `apphost.ResolveWritablePath`) — e.g. `mymatasan` resolves its at-rest encryption key path this way.
  - apps bind their own user store via `deps.Access.SetResolver(...)` during `RegisterAppRoutes`
- `Restarter`
  - one-method primitive (`Restart(reason string)`) that gracefully restarts the process: it triggers the host's normal shutdown sequence and then relaunches a fresh instance from the current on-disk executable
  - general-purpose — used by the `mymatasan` factory reset and intended for self-update (swap the binary, then call `Restart`); the first call wins, later calls are no-ops
- `SharedAPIConfig`
  - controls which shared route groups the host mounts for a selected app
  - `AccessRbac: true` (the default): seeds `superadmin` + `viewer` roles, seeds viewer defaults, and mounts the `/api/access-rbac` management surface with the accessrbac middleware
  - `mymatasan` opts out by returning `SharedAPIConfig{Version: true}` from `SharedAPIs()`
- `SharedAPIConfigurator`
  - optional app interface for resource apps that should expose only a subset of shared APIs
- `WebRouteRegistrar`
  - optional app interface for non-API routes registered before static asset fallback
- `AppConfigDecoder`
  - optional app interface (`DecodeAppConfig(raw []byte, dataDir string) error`) for an app
    that owns config blocks of its own, decoded from the same raw `config.json` document the
    host already parsed (`config.AppConfigModel.Raw()`) rather than being added to the
    shared model. `mymatasan` implements it (`apps/mymatasan/config`, see
    `docs/modules/apps/mymatasan/config/config.go.md`) for its `camera`/`decoder`/`stream`/
    `vision`/`health`/`recording` blocks. `run.go` calls it, when implemented, after the
    shared config is loaded and normalized and before any route is registered; a returned
    error aborts startup — a config the app cannot understand must not boot on silent
    defaults. The blocks stay at the top level of `config.json`, not nested under an `"app"`
    key, so no deployed config file has to change; only ownership moves.
- `ShutdownFunc`
  - optional app worker shutdown callback

## Notes

- New apps implement this interface to reuse startup/runtime behavior without rewriting a large `main.go`.
- Apps that do not implement `SharedAPIConfigurator` get the full shared API surface by default.
- Apps can implement `WebRouteRegistrar` when they need protected root pages or callback routes outside the `/api` router.
