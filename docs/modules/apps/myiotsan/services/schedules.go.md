# Module: apps/myiotsan/services/schedules.go

## Purpose

Fires scenes and commands on the CLOCK — the automation a telemetry rule cannot express, because
its trigger is a time (or sunrise/sunset), not a reading. It fires through the same actuation path
everything else does: a scheduled scene run and a scheduled command both flow through
`SceneService.Run`/`CommandService.Issue`, so every gate still applies. A schedule can command
nothing an admin has not made commandable. See `docs/MYIOTSAN_PLAN.md` §8h.

## Key Type: ScheduleService

```go
func NewScheduleService(db dbsql.IDbCrud, scenes *SceneService, commands *CommandService, logf func(string, ...any)) *ScheduleService

func (s *ScheduleService) List(ctx) ([]*entities.Schedule, error)
func (s *ScheduleService) Create(ctx, SaveScheduleRequest, actor) (*entities.Schedule, error)
func (s *ScheduleService) Update(ctx, id, SaveScheduleRequest, actor) (*entities.Schedule, error)
func (s *ScheduleService) Delete(ctx, id) error
func (s *ScheduleService) GetLocation(ctx) (lat, lon float64, ok bool)
func (s *ScheduleService) SetLocation(ctx, lat, lon float64, actor int64) error
func (s *ScheduleService) DueAt(sched *entities.Schedule, now time.Time, lat, lon float64, haveLoc bool) bool
func (s *ScheduleService) Tick(ctx, now time.Time)
func (s *ScheduleService) RunNow(ctx, id, actor int64, actorName string) error
```

- `depends` on a `commandIssuer` (`services/scenes.go.md`), not `*CommandService` directly — same
  test-without-a-database reasoning `SceneService` uses.
- `validate` refuses a save with no name, a `"clock"` trigger with no valid `TimeOfDay`, a sun
  trigger with no site location yet set (`GetLocation`), or a `TargetType` that names neither a
  scene nor a device+command.
- `Update` **preserves `LastFiredAt` across an edit** — editing a schedule's time must not reset
  its double-fire guard and risk a second fire in the same minute it was edited.

## Location (RuntimeSetting)

`GetLocation`/`SetLocation` read/write a single `RuntimeSetting` row under key `"site.location"`
(`locationKey`), JSON `{latitude, longitude}`. **Deliberately not a field on `Schedule` itself** —
the location belongs to the *site*, not to any one schedule, and it is operator-set runtime data
(picked on a map), so it belongs in the settings store rather than `config.json`. `SetLocation`
validates `-90..90`/`-180..180` before writing. Exposed over HTTP as `GET`/`PUT
/api/settings/location`, admin-only (`services/rbac.go.md`) — it is the input that decides WHEN a
sun-triggered automation fires, gated the same as the automation itself.

## Evaluation & firing

- `fireInstant(sched, day, lat, lon, haveLoc)` resolves the local wall-clock instant a schedule
  should fire ON THE DAY of `day`: `TimeOfDay` for a clock trigger, or the local sunrise/sunset
  (`services/sun.go.md`) shifted by `OffsetMinutes` for a sun trigger. Returns `ok=false` when it
  cannot be computed (bad clock string, missing location, polar day) — the caller treats that as
  "does not fire today", never as a fabricated time.
- `DueAt` reports whether a schedule should fire in the **minute** of `now`: enabled, its weekday
  matches `Days`, and its resolved fire instant falls in the same minute `now` does. Matching is
  minute-granular because the scheduler ticks once a minute (`app.go`'s `"myiotsan.scheduler"`
  task, `app/app.go.md`).
- `Tick(ctx, now)` fires every due schedule once for the minute of `now`. **The double-fire guard
  is `LastFiredAt`, rounded to the unix-minute and PERSISTED before firing**: a tick that runs
  twice in a minute, or a restart inside the firing minute, cannot fire a schedule a second time —
  the same cooldown-survives-a-restart lesson `IotRule`'s alert cooldown already applies
  (`services/rule_engine.go.md`), now applied to time. If persisting the guard fails, `Tick`
  deliberately does **not** fire — "better a missed fire than a double one" is the same physical-
  action-cannot-be-undone reasoning behind never auto-retrying a command.
- `RunNow` test-fires a schedule immediately, ignoring its trigger entirely — the UI's "test"
  button. It still goes through the gated actuation path (`fireAs`), so a test cannot do anything
  the schedule itself could not; it does not touch `LastFiredAt`.
- `fire`/`fireAs` set the actor to `(0, "schedule:<name>")` for a real (non-test) fire — a
  scheduled fire has no user behind it, and without the synthetic name the audit trail
  (`DeviceCommand.RequestedByName`, a scene's `ActionResult`) would read "System" instead of
  attributing it to the schedule that acted. `RunNow` instead passes through the calling admin's
  own `actor`/`actorName`.
- `daysMatch(days, wd)` — an empty `Days` list means every day; otherwise `wd` must appear in the
  comma-separated `0`(Sunday)`..6`(Saturday) list.

## Notes

- Wired in `app.go`'s `RegisterAppRoutes`, constructed right after `SceneService`. The
  `"myiotsan.scheduler"` `safego.Supervise`d task aligns its first tick to the next whole-minute
  boundary, then calls `Tick` once a minute (`schedulerInterval` = `time.Minute`). See
  `app/app.go.md`.
- Exposed over HTTP by `apis/schedules.go.md`. RBAC: readable by viewer/operator; authoring,
  `RunNow`, and the location endpoints are admin-only (`services/rbac.go.md`).
- Covered by `schedules_test.go.md` (hermetic `DueAt` boundary/weekday/double-fire-guard tests) and
  `sun_test.go.md` (the sunrise/sunset math `fireInstant` depends on for a sun trigger).
