# Module: apps/myiotsan/services/rule_engine_test.go

## Purpose

Pins the rule evaluator's exact behaviour down, including the two production bugs this port
was built specifically not to repeat.

## Responsibilities

- `TestRule_FiresWhenTheThresholdIsCrossed` / `TestRule_DoesNotFireBelowTheThreshold` — basic
  `above` predicate plus that a fired decision always carries a `Reason`.
- `TestRule_DebounceRequiresConsecutiveSamples` / `TestRule_DebounceResetsOnAClearSample` — N-in-
  a-row required to fire; a clearing sample resets the counter (otherwise three spikes an hour
  apart would add up to an alert).
- `TestRule_HysteresisStopsFlapping` — a value hovering on the threshold does not flap; it must
  clear the hysteresis band to re-arm, and a later excursion after genuine recovery is a new
  event.
- `TestRule_CooldownSuppressesRefiring` — no re-fire inside the cooldown window; fires again once
  it has elapsed.
- **`TestRule_CooldownSurvivesRestart`** — THE regression test for mymatasan's actual shipped
  bug (`LastTriggeredAt` loaded and never read → alert storm on every restart). A brand-new
  engine seeded via `SeedCooldown` from a simulated durable record must not re-fire while the
  seeded cooldown is still active.
- **`TestRule_TagScopedRuleFiresPerDeviceNotOnce`** and **`TestRule_CooldownIsPerDevice`** — THE
  missed-alert regression tests. A tag-scoped rule over 10 devices must fire independently for
  all 10 (not just the first), and each device's cooldown must be independent of the others'.
  Found by pointing an offline rule at 20 real devices and watching exactly one fire.
- `TestRule_StuckCatchesAFrozenSensor` — an unmoved value across the window fires; a moved one
  does not.
- `TestRule_RateUsesRealElapsedTimeNotTheNominalWindow` — a fast change measured across a short
  real interval is correctly identified as fast, rather than diluted across the nominal window.
- `TestRule_OfflineFiresOnSilence` — silence past the window fires; short of it does not.
- `TestRule_DisabledNeverFires`.
- `TestRule_ScheduleSpansMidnight` — the overnight window (`22:00`-`06:00`) fires inside it
  (including across midnight) and not outside it.
- `TestRule_ScheduleResetsTheDebounceOnTheWayOut` — a debounce count from inside a schedule
  window does not carry over into the next window.

## Notes

- Pure unit tests against `rule_engine.go`; synthetic `Observation`s and `time.Date(...)`
  fixtures, no database.
