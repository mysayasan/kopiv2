# Module: apps/myiotsan/entities/schedule.go

## Purpose

Fires a scene (or a single device command) at a time — the automation half a telemetry rule
cannot express, because its trigger is the CLOCK (or the sun), not a reading. Like a command, a
schedule fires through the actuation chokepoint (`services.CommandService.Issue`, directly or via
`services.SceneService.Run`), so every gate still applies: a schedule can command nothing an admin
has not made commandable. See `services/schedules.go.md` and `docs/MYIOTSAN_PLAN.md` §8h.

## Fields

- `Name` — required. `Enabled` — a disabled schedule is kept and editable but never fires
  (`services.ScheduleService.DueAt`/`Tick`).
- `TriggerType` — `"clock"` (at `TimeOfDay` on the listed `Days`), `"sunrise"` or `"sunset"` (at
  the local sun event ± `OffsetMinutes`, computed from the site's latitude/longitude —
  `services/sun.go.md`).
- `TimeOfDay` — `"HH:MM"`, 24h, local; ignored for a sun trigger.
- `OffsetMinutes` — shifts a sun trigger (e.g. `-30` = half an hour before sunset); ignored for a
  clock trigger.
- `Days` — comma-separated weekday list (`0`=Sunday..`6`=Saturday); empty means every day.
- `TargetType` — `"scene"` (uses `SceneId`) or `"command"` (uses `DeviceId`/`CommandName`/`Value`
  — the same fields a `SceneAction` carries).
- `LastFiredAt` — the unix-**minute** the schedule last fired; the double-fire guard, persisted so
  a tick running twice in the same minute, or a restart inside the firing minute, cannot fire the
  schedule a second time. This is the same cooldown-survives-a-restart lesson `IotRule`'s alert
  cooldown already applies, now applied to time (`services/rule_engine.go.md`).
- Audit fields: `CreatedBy`/`CreatedAt`/`UpdatedBy`/`UpdatedAt`.

## Notes

- Bootstrap creates this table from the registered entity (`app/app.go`'s `Entities()`).
- The site latitude/longitude a sun trigger needs is **not** a field on this entity — it is a
  single shared setting (`RuntimeSetting`, key `"site.location"`), read/written by
  `services.ScheduleService.GetLocation`/`SetLocation`. Operator-set runtime data (someone picks
  it on a map) belongs in the settings store, not `config.json`.
- `services.ScheduleService.fire`/`fireAs` set the firing actor to `(0, "schedule:<name>")` — a
  synthetic principal with no local account — so every downstream audit row
  (`DeviceCommand.RequestedByName`, a scene's `ActionResult`) attributes the action to the
  schedule by name rather than reading "System".
