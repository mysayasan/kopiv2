# Module: apps/mymatasan/services/continuity_settings.go

## Purpose

Implements `IContinuitySettingsService`, the runtime-editable configuration for the recording-continuity monitor (`services/recording_continuity.go.md`): whether it runs, how often it sweeps, how much of an hour must be on disk to count as covered, and how many consecutive bad/good hours flip the alert.

## Responsibilities

- `ContinuitySettings` — `Enabled`, `IntervalMs`, `MinCoveragePercent`, `FailureThreshold`, `RecoveryThreshold`.
- `DefaultContinuitySettings()` — enabled, 10-minute sweep, 95% minimum coverage, 2 bad hours to alert, 1 good hour to clear.
- `normalizeContinuitySettings(in)` — replaces any zero/out-of-range field with its default rather than rejecting the save: an interval `<= 0` or a percentage outside `(0, 100]` cannot mean anything (0% would score every hour covered and make the monitor a no-op; above 100% would alarm on every hour).
- `NewContinuitySettingsService(repo)` — persists to the same `runtime_setting` table as the other monitor settings, under the `"continuity"` key, upserting via `GetByUnique`/`UpdateById`/`Create`. `Get` seeds and returns the defaults on first read (no row yet) rather than erroring.

## Notes

- Read live on every sweep by the monitor, so a change from Settings takes effect without a restart — deliberate, since retuning this monitor is done by watching it run on a real site.
- Exposed over HTTP at `GET`/`PUT /api/settings/continuity` (`apis/settings.go.md`); the save is audited (`services.ActionSettingsChange`, target `"continuity"`) because changing this configuration changes what the system will and will not alarm about.
- 95% rather than 100% allows roughly three minutes of legitimate loss per hour — segment rollover, a recorder restart, the remux queue — while still catching a camera that recorded 40 of 60 minutes; see `services/recording_continuity.go.md` for the full reasoning.
- No dedicated test file; covered indirectly through `recording_continuity_test.go`'s settings-driven sweep behaviour.
