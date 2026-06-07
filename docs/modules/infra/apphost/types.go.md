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
  - includes shared runtime primitives such as database, cache, auth, RBAC, app registry, logger, and scheduler
- `SharedAPIConfig`
  - controls which shared route groups the host mounts for a selected app
- `SharedAPIConfigurator`
  - optional app interface for resource apps that should expose only a subset of shared APIs
- `ShutdownFunc`
  - optional app worker shutdown callback

## Notes

- New apps implement this interface to reuse startup/runtime behavior without rewriting a large `main.go`.
- Apps that do not implement `SharedAPIConfigurator` get the full shared API surface by default.
