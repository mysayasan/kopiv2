# Module: apps/mymatasan/apis/anomaly.go

## Purpose

HTTP surface for the statistical anomaly monitor (Dashboard Intelligence Phase 3): runtime settings and an on-demand preview scan.

## Responsibilities

- `NewAnomalyApi(router, settings services.IAnomalySettingsService, scanner services.IAnomalyScanner, camera services.ICameraService)` — registers under `/anomaly`:
  - `GET /anomaly/settings` — current `AnomalySettings`.
  - `PUT /anomaly/settings` — save `AnomalySettings` (normalized/clamped by the service).
  - `GET /anomaly/scan` — runs `AnomalyScan` for one closed hour (`?hour=` unix seconds, defaults to the most recently completed hour; `?tzOffset=` viewer minutes, clamped ±840) at the currently-saved sensitivity/min-activity, and returns `{hourStart, findings: [{cameraId, cameraName, direction, actual, lo, hi, median, samples, hourStart}]}` — enriched with camera display names so the Settings UI can preview what would alert without waiting for the background monitor.

## Notes

- Independent of `AnalyticsMonitor`'s own state: a preview `scan` never touches the monitor's debounce/cooldown counters and never publishes a notification — it's read-only.
- Registered on the protected router alongside `NewNotificationApi`; `PUT /anomaly/settings` follows the same admin-write gating as other Settings-backed endpoints.
