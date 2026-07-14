# Module: apps/myiotsan/services/deadband_test.go

## Purpose

Pins the deadband gate's exact admission rules down, including the two easiest to get wrong:
rebaselining against the last STORED value (not the original), and per-series independence.

## Responsibilities

- `TestDeadband_FirstSampleAlwaysPasses` — nothing to compare against yet.
- `TestDeadband_SuppressesNoiseBelowTheBand` — a probe jittering in the last decimal place
  produces zero stored rows.
- `TestDeadband_AdmitsARealMove` — a move past the deadband is stored, and the new baseline is
  what the next comparison uses (not the original value).
- `TestDeadband_SlowDriftIsSuppressedUntilItAccumulates` — a drift that never exceeds the
  deadband in one step still accumulates roughly one stored row per `Deadband` of total travel;
  pinned so nobody "fixes" this into storing every step.
- `TestDeadband_HeartbeatForcesAFlatLineThrough` — an unchanged value is stored once the
  heartbeat elapses, and the heartbeat clock restarts from the last STORED row.
- `TestDeadband_ZeroDeadbandStoresTransitionsNotRepeats` — a door contact (`Deadband: 0`) stores
  every transition but not an identical republish.
- `TestDeadband_StringKeysCompareByEquality` — no magnitude concept for strings.
- `TestDeadband_SeriesAreIndependent` — a different device or a different key is a different
  series; noise on one sensor cannot suppress another's readings.
- `TestDeadband_ForgetDropsADeletedDevice` — `Forget` removes only that device's series, and
  its next sample after that is treated as first-seen again.

## Notes

- Pure unit tests against `deadband.go`; synthetic timestamps (`sec`/`min` constants), no clock,
  no database.
