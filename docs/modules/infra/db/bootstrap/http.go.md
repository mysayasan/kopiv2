# Module: infra/db/bootstrap/http.go

## Purpose

Exposes reusable setup/status HTTP handlers for the bootstrap system.

## Handlers

- `StatusHandler`
  - returns the latest bootstrap status as JSON
- `SetupPageHandler`
  - returns a minimal HTML status page for browser access

## Notes

- The handlers are intentionally generic so apps can mount them at their configured setup path.
- The page renders the status snapshot and JSON payload returned by the bootstrap engine.
