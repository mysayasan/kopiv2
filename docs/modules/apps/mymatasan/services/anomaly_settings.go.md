# Module: apps/mymatasan/services/anomaly_settings.go

## Purpose

Implements `IAnomalySettingsService`, the runtime-editable configuration for the anomaly monitor (Dashboard Intelligence): per-camera statistical spike / "unusual silence" alerting, or a no-history-required fixed-threshold tier, tuned from Settings and applied live without a restart.

## Responsibilities

- `AnomalySettings` fields: `Enabled` (off by default — only meaningful once weeks of activity history have accumulated), `Mode` (`"smart"` = statistical per-camera baseline, `"manual"` = fixed site-wide hourly thresholds; defaults/normalizes to `"smart"`), `ManualUpper`/`ManualLower` (manual-mode per-hour total-event thresholds; `0` disables that side, clamped ≥ 0), `Sensitivity` (band half-width in robust sigmas `k`, typical 2.0–3.0, clamped 1.0–6.0, smart mode only), `DetectHigh`/`DetectLow` (which breach directions alert, smart mode only), `MinActivity` (suppresses "unusual silence" for slots whose normal/median activity is below this, smart mode only), `RequireConsecutive` (consecutive anomalous hours before the first alert — a blip debounce, clamped 1–12), `CooldownHours` (repeat-alert suppression per camera+direction, clamped 1–168), `CheckIntervalMs` (monitor tick cadence, default 5 min, clamped 30s–1h).
- `NewAnomalySettingsService(repo, defaults)` — persists/reads a single JSON blob under the `anomalyDetection` runtime-setting key (`anomalySettingsKey`).
- `Get(ctx)` — seeds the row with `defaults` via `Save` on first read (no-result case).
- `Save(ctx, settings)` — normalizes (`normalizeAnomalySettings`) then upserts.
- `DefaultAnomalySettings()` — `{Enabled: false, Mode: "smart", ManualUpper: 0, ManualLower: 0, Sensitivity: 3.0, DetectHigh: true, DetectLow: true, MinActivity: 3, RequireConsecutive: 1, CooldownHours: 6, CheckIntervalMs: 300000}`.

## Notes

- Read live each tick by `AnalyticsMonitor` (`analytics_monitor.go`), so toggling/retuning from Settings → AI takes effect without a restart.
- Exposed via `GET/PUT /api/anomaly/settings` (`apis/anomaly.go`).
