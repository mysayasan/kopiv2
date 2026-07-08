# Module: apps/mymatasan/services/anomaly_settings.go

## Purpose

Implements `IAnomalySettingsService`, the runtime-editable configuration for the statistical anomaly monitor (Dashboard Intelligence Phase 3): per-camera spike / "unusual silence" alerting tuned from Settings, applied live without a restart.

## Responsibilities

- `AnomalySettings` fields: `Enabled` (off by default — only meaningful once weeks of activity history have accumulated), `Sensitivity` (band half-width in robust sigmas `k`, typical 2.0–3.0, clamped 1.0–6.0), `DetectHigh`/`DetectLow` (which breach directions alert), `MinActivity` (suppresses "unusual silence" for slots whose normal/median activity is below this, so genuinely-quiet hours aren't flagged), `RequireConsecutive` (consecutive anomalous hours before the first alert — a blip debounce, clamped 1–12), `CooldownHours` (repeat-alert suppression per camera+direction, clamped 1–168), `CheckIntervalMs` (monitor tick cadence, default 5 min, clamped 30s–1h).
- `NewAnomalySettingsService(repo, defaults)` — persists/reads a single JSON blob under the `anomalyDetection` runtime-setting key (`anomalySettingsKey`).
- `Get(ctx)` — seeds the row with `defaults` via `Save` on first read (no-result case).
- `Save(ctx, settings)` — normalizes (`normalizeAnomalySettings`) then upserts.
- `DefaultAnomalySettings()` — `{Enabled: false, Sensitivity: 3.0, DetectHigh: true, DetectLow: true, MinActivity: 3, RequireConsecutive: 1, CooldownHours: 6, CheckIntervalMs: 300000}`.

## Notes

- Read live each tick by `AnalyticsMonitor` (`analytics_monitor.go`), so toggling/retuning from Settings → AI takes effect without a restart.
- Exposed via `GET/PUT /api/anomaly/settings` (`apis/anomaly.go`).
