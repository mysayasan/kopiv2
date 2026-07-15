# Module: apps/myiotsan/services/sun_test.go

## Purpose

Pins `SunriseSunset` against published almanac times, and against the one case where it must
refuse to answer.

## Responsibilities

- `TestSunriseSunset_London` — three dates (spring equinox, summer solstice, winter solstice) at
  London's coordinates, checked against published sunrise/sunset times to a 5-minute tolerance
  (the algorithm is good to ~1-2 minutes; the tolerance leaves room for the ±0.833° horizon
  convention and rounding).
- `TestSunriseSunset_PolarDayHasNoEvent` — at Svalbard on the summer solstice (midnight sun), `ok`
  must be `false` rather than the function returning a fabricated time a schedule would fire on.
- `assertClock` — a helper comparing only hour:minute (not the date), tolerant to `tolMin` minutes.

## Notes

- No database, no `ScheduleService` — pure arithmetic against `time.Time` values.
- `services/schedules_test.go.md`'s `TestSchedule_DueAtSunsetWithOffset` derives its expected fire
  time FROM `SunriseSunset` directly rather than a second hardcoded value, so a ±1-minute change in
  this module's math cannot make that test brittle.
