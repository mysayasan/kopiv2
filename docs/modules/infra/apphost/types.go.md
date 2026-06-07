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
  - includes the core `UserLogin` service so app-local routes can build app-specific DTO adapters without duplicating repository wiring
- `ShutdownFunc`
  - optional app worker shutdown callback

## Notes

- New apps implement this interface to reuse startup/runtime behavior without rewriting a large `main.go`.
