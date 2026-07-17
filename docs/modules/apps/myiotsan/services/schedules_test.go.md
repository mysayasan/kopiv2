# Module: apps/myiotsan/services/schedules_test.go

## Purpose

Pins `ScheduleService.DueAt` — the heart of the scheduler — down to the minute boundary, the
weekday filter, and the double-fire guard's arithmetic, all hermetically (no database, no
network).

## Responsibilities

- `TestSchedule_DueAtClockBoundaries` — a `"clock"` schedule at `07:30` fires at exactly `07:30`,
  not at `07:29` or `07:31`, not at an unrelated hour; a `Days` filter (`"1"` = Monday-only)
  correctly excludes a Wednesday and a `"3,6"` list correctly includes it; a disabled schedule
  never fires regardless of time.
- `TestSchedule_DueAtSunsetWithOffset` — a `"sunset"` schedule with `OffsetMinutes: -30` fires at
  exactly sunset-minus-30-minutes (the expected instant is derived from `SunriseSunset` itself, not
  a second hardcoded time — see `sun_test.go.md`), not two minutes later, and never fires at all
  when `haveLoc` is `false` (no site location set).
- `TestSchedule_DoubleFireGuard` — exercises the `LastFiredAt == thisMin` guard arithmetic `Tick`
  uses directly: the same minute produces the same guard value (would skip), the next minute
  produces a different one (eligible again).

## Notes

- Constructs a bare `ScheduleService{logf: ...}` and calls `DueAt` directly — no
  `NewScheduleService`, no database, no `SceneService`/`CommandService`.
- The database-dependent half (`Tick`'s list-and-persist loop, `Create`/`Update`/`Delete`,
  `GetLocation`/`SetLocation`) is exercised live instead — see `app/app.go.md`'s home-automation
  verification note (create + test-fire a sunset schedule).
