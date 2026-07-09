# Module: apps/mymatasan/apis/anomaly.go

## Purpose

HTTP surface for the statistical anomaly monitor (Dashboard Intelligence Phase 3): runtime settings and an on-demand preview scan.

## Responsibilities

- `NewAnomalyApi(router, settings services.IAnomalySettingsService, scanner services.IAnomalyScanner, camera services.ICameraService)` — registers under `/anomaly`:
  - `GET /anomaly/settings` — current `AnomalySettings`.
  - `PUT /anomaly/settings` — save `AnomalySettings` (normalized/clamped by the service).
  - `GET /anomaly/scan` — runs one closed hour's scan (`?hour=` unix seconds, defaults to the most recently completed hour; `?tzOffset=` viewer minutes, clamped ±840) and returns `{hourStart, findings: [{cameraId, cameraName, direction, actual, lo, hi, median, samples, hourStart}]}` — enriched with camera display names so the Settings UI can preview what would alert without waiting for the background monitor. The tier used depends on the saved `AnomalySettings.Mode`: `"manual"` runs `ManualScan` (fixed site-wide hourly thresholds, `cameraId: 0` findings) instead of the default `AnomalyScan` (per-camera statistical baseline, at the currently-saved sensitivity/min-activity).

## Notes

- Independent of `AnalyticsMonitor`'s own state: a preview `scan` never touches the monitor's debounce/cooldown counters and never publishes a notification — it's read-only.
- Manual-mode findings have `cameraId: 0` (site-wide, not per-camera) since `ManualScan` compares the whole system's hourly event total against `ManualUpper`/`ManualLower`, not a learned per-camera baseline — this is the no-history-required tier, usable from day one.
- Registered on the protected router alongside `NewNotificationApi`; `PUT /anomaly/settings` follows the same admin-write gating as other Settings-backed endpoints.
