# Module: apps/mymatasan/services/tamper_settings.go

## Purpose

Implements `ITamperSettingsService`, the runtime-editable configuration for the camera tamper monitor (`services/camera_tamper_monitor.go.md`): whether it runs, how often it samples, and the thresholds behind each of the three things it detects (frozen, covered, moved).

## Responsibilities

- `TamperSettings` — `Enabled`, `IntervalMs`, `FailureThreshold` (consecutive abnormal samples before alerting), `FrozenSeconds`/`FrozenMaxDifference` (how long and how identical a picture must be to count as frozen), `CoveredRatio` (fraction of the camera's own recent median edge energy below which the view counts as blocked), `MovedDistance` (luma-histogram distance that counts as pointed somewhere else).
- `DefaultTamperSettings()` — enabled, 30s samples, 3 consecutive abnormal samples to alert, 120s/0.0005 for frozen, 0.15 covered ratio, 0.55 moved distance. Every number leans conservative, because the failure mode that kills this feature is not a missed tamper — it is an operator muting it after a week of false alarms, after which it protects nothing.
- `normalizeTamperSettings(in)` — replaces any value outside its meaningful range with the default rather than rejecting the save (e.g. `CoveredRatio` outside `(0, 1)` cannot mean anything: 0 never fires, 1 or more fires on every sample including a normal one).
- `NewTamperSettingsService(repo)` — persists to the `runtime_setting` table under the `"tamper"` key, same upsert shape as `continuity_settings.go`.

## Notes

- Read live on every sweep — retuning this monitor is done by watching it run on a real site, so a restart would be in the way.
- Exposed over HTTP at `GET`/`PUT /api/settings/tamper` (`apis/settings.go.md`); the save is audited (`services.ActionSettingsChange`, target `"tamper"`).
- No dedicated test file; covered indirectly through `camera_tamper_monitor_test.go`'s settings-driven detection behaviour ("nonsense settings normalize").
