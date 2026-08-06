# Module: apps/myseliasan/services/agent_schedule.go

## Purpose

The daily digest scheduler: "every day at HH:00 local" cannot be expressed as a fixed-interval
ticker (`app.go`'s `periodic` helper runs immediate+interval), so this is a dedicated sleep-until,
fire-once, repeat loop.

## `RunDigestSchedule(ctx, digests *DigestService, runtimeSettings, cfg func() config.AgentConfigModel, logf)`

Starts the loop under `safego.Supervise(ctx, "myseliasan.agent-digest", s.run)` and returns
immediately. `cfg` is read every iteration so a config change (after a restart) is picked up
without re-wiring the scheduler.

## The double-fire guard

Persisted as a local **date string** (`agent.digest.lastRun` in `RuntimeSetting`), not a
timestamp: a restart at 07:00:30 must not re-run a digest the pre-restart process already
generated at 07:00:05, and "did today's digest already run?" is exactly a date comparison in the
server's local zone.

- `nextDigestRun(now, localHour, lastRunDate)` — today at `localHour` if that instant is still
  ahead **and** today's digest hasn't run yet, else tomorrow at `localHour`. Uses `time.Date` in
  the local zone (not `+24h`) so a 23- or 25-hour DST day still fires at `HH:00` local.
- On wake, a second `lastRunDate` check guards against a concurrent/pre-restart instance having
  already generated today's digest while this instance slept.
- A failed generation retries after `digestScheduleRetry` (30 minutes) within the same day, rather
  than waiting until tomorrow.
- `digestEnabled`/`digestHour` read the config pointers (`Digest.Enabled *bool`,
  `Digest.LocalHour *int`) with the documented defaults (enabled; hour 7 — "a morning digest, not
  a midnight one").

## Notes

- Disabled (`digestEnabled(cfg) == false`): the loop idles, re-checking hourly — belt-and-braces,
  since a settings change only takes effect after a restart anyway (`services/settings_apply.go`).
- `sleepCtx(ctx, d)` sleeps for `d`, returning `false` (loop exits) if `ctx` is cancelled first.
- See `services/agent_digest.go.md` for what `Generate("daily", 0)` does, and `app.go`'s wiring
  (right after `apis.NewAgentApi`) for the actor id `0` convention (scheduler-generated).
