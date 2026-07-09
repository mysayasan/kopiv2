# Module: apps/mymatasan/services/analytics_monitor.go

## Purpose

Implements `AnalyticsMonitor`, the statistical anomaly monitor (Dashboard Intelligence Phase 3): a 4th background monitor (alongside camera health, machine health, and vision) that scores each recently-closed hour against per-camera learned baselines and raises `analytics.anomaly` notifications for spikes and "unusual silence".

## Responsibilities

- `IAnomalyScanner` — narrow interface (`AnomalyScan(ctx, hourStart, tzOffsetSec, k, minActivity)` and `ManualScan(ctx, hourStart, upper, lower)`) satisfied by `domain/notification.Service`, so this package doesn't depend on the full notification service surface.
- `NewAnalyticsMonitor(scanner, notifier INotificationPublisher, settings IAnomalySettingsService, camera ICameraService)` — `camera` may be `nil` (names fall back to `"Camera N"`); the others are required.
- `Start(ctx)` runs the monitor loop in its own goroutine, ticking on `AnomalySettings.CheckIntervalMs` (re-read live each tick; falls back to 5 min if unset/invalid).
- `tick(ctx)` — no-op unless `Enabled`; otherwise scores the **most recently closed** whole hour exactly once (`lastEvalHour` guard), so an in-progress hour is never mistaken for a drop in activity. `AnomalySettings.Mode` selects the tier: `"manual"` calls `ManualScan` (fixed site-wide hourly thresholds, `ManualUpper`/`ManualLower`, no history needed); anything else (default `"smart"`) calls `AnomalyScan` (per-camera statistical baseline). Baselines are seasonal by local clock, so smart-mode scoring uses the server's local timezone offset.
- Per-(camera, direction) debounce/cooldown (`anomalyState{streak, lastAlertHour}`): a finding only alerts once `streak >= RequireConsecutive` consecutive anomalous hours and at least `CooldownHours` have passed since the last alert for that camera+direction; a camera+direction not anomalous this hour resets its streak. Manual-mode findings use `CameraId: 0` as their debounce key (one site-wide streak/cooldown).
- `emit(ctx, finding)` — publishes one `analytics.anomaly` category, `Warning` severity notification. `finding.CameraId == 0` (manual/site-wide) emits `"High overall activity"` / `"Low overall activity"` against the configured threshold; a per-camera finding emits the existing `"Unusual activity spike — <camera>"` / `"Unusual quiet — <camera>"` title/body against its learned band. Structured `Data` carries `direction`, `actual`, `expectedLo/Hi`, `median`, `hourStart`, `samples`.

## Notes

- `analytics.anomaly` flows into the unified notification feed and every configured delivery destination (webhook/Telegram/MQTT) like any other category, and appears in the dashboard breakdowns.
- Mirrors the health monitors' pattern: settings read live each tick, so retuning from Settings → AI (including switching `Mode`) takes effect without a restart.
- `GET /api/anomaly/scan` (`apis/anomaly.go`) runs the same tier (`AnomalyScan` or `ManualScan`, per `Mode`) on demand for a Settings-UI preview, independent of this monitor's own debounce/cooldown state (a preview scan never fires a real notification).
- Wired in `app.go`: `services.NewAnalyticsMonitor(notificationService, notificationService, anomalySettingsService, cameraService).Start(monitorCtx)` — the notification service satisfies both `IAnomalyScanner` (reads) and `INotificationPublisher` (the alert it emits).
