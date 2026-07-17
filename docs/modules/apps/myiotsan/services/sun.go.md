# Module: apps/myiotsan/services/sun.go

## Purpose

Sunrise/sunset by the standard NOAA / "sunrise equation" algorithm — pure arithmetic, no
dependency, no network. A schedule that fires "30 minutes before sunset" needs the sun's times for
the site, and an air-gapped appliance cannot ask an API for them, so it computes them. See
`services/schedules.go.md` (the consumer) and `docs/MYIOTSAN_PLAN.md` §8h.

## Key Function: SunriseSunset

```go
func SunriseSunset(date time.Time, lat, lon float64) (rise, set time.Time, ok bool)
```

Returns the sunrise and sunset (UTC) for the calendar day of `date` at the given latitude/
longitude. Longitude is **east-positive** (the geographic convention); latitude is north-positive.
`ok` is `false` at a polar day/night, where the sun never crosses the horizon and neither time is
defined — the caller (`ScheduleService.fireInstant`) treats that as "cannot resolve, do not fire"
rather than fabricating a bogus time a schedule would fire on.

Accuracy is ~1 minute at the mid-latitudes this app targets, which is far finer than a per-minute
scheduler tick (`services/schedules.go.md`) can act on anyway.

## Notes

- Pure function, no I/O, no state — a deliberate design choice for an on-prem/air-gapped
  appliance that cannot call out to a sunrise-sunset API.
- `julianDay`/`fromJulian`/`sinDeg`/`cosDeg` are private arithmetic helpers (Julian Date
  conversion, degree-based trig) with no independent behavior worth documenting past the
  algorithm's own name.
- Covered by `sun_test.go.md` against published almanac times for London, plus the Svalbard
  midsummer polar-day case.
- Consumed exclusively by `services.ScheduleService.fireInstant` for a `"sunrise"`/`"sunset"`
  `TriggerType` (`services/schedules.go.md`); a `"clock"` trigger never calls this.
