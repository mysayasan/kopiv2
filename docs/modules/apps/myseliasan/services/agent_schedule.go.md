# Module: apps/myseliasan/services/agent_schedule.go

## Purpose

The digest scheduler: "every day at HH:00 local" (and, now, "every Weekday at HH:00 local" too)
cannot be expressed as a fixed-interval ticker (`app.go`'s `periodic` helper runs
immediate+interval), so this is a dedicated sleep-until, fire-once, repeat loop that runs **both
cadences** from one goroutine.

## `RunDigestSchedule(ctx, digests *DigestService, runtimeSettings, cfg func() config.AgentConfigModel, gate func() bool, logf)`

Starts the loop under `safego.Supervise(ctx, "myseliasan.agent-digest", s.run)` and returns
immediately. `cfg` is read every iteration so a config change (after a restart) is picked up
without re-wiring the scheduler.

`gate` is a new trailing parameter (deployment mode / Phase 1 multi-instance safety): reports
whether THIS instance should generate. `app.go` passes `deps.Leader.IsLeader`; `nil` means
"always", which is correct for a single instance and is what every caller passed before this
existed. `digestSchedule.shouldRun()` returns `gate == nil || gate()`.

## Leader gating vs. the date-watermark guard

The existing `digestLastRunKey`/`digestLastWeeklyKey` guard (below) is a read-then-write with no
lock: two instances waking in the same second both read "not run today" and both generate. Because
a digest is an LLM call and an operator-visible artefact, a duplicate here costs real money and
real confusion, not just wasted CPU — worse than the notification-rollup race this same Phase
fixes elsewhere. `shouldRun()` is checked **after the sleep**, at the moment of generating, not at
the top of the loop or once at startup: the wait between wake-ups is hours long, and leadership can
move to or away from this instance during it, so the only moment worth asking about is the moment
just before acting.

## Two cadences, two guard keys

The daily digest (`digestEnabled`, default **on**) and the weekly digest
(`weeklyDigestEnabled`, default **off** — opt-in) run independently, each persisted as its own
local **date string**, not a timestamp — a restart at 07:00:30 must not re-run a digest the
pre-restart process already generated at 07:00:05, and "did this cadence already run?" is exactly
a date comparison in the server's local zone:

- `digestLastRunKey = "agent.digest.lastRun"` — the daily guard.
- `digestLastWeeklyKey = "agent.digest.lastWeeklyRun"` — the weekly guard, kept separate so the
  two cadences can fire on the same morning (a Monday that is also the daily 07:00 slot) without
  one guard masking the other.

Each loop iteration computes **both** cadences' next occurrence and sleeps until whichever is
nearer, then fires that one:

- `nextDigestRun(now, localHour, lastRunDate)` — today at `localHour` if that instant is still
  ahead **and** today's digest hasn't run yet, else tomorrow at `localHour`. Uses `time.Date` in
  the local zone (not `+24h`) so a 23- or 25-hour DST day still fires at `HH:00` local.
- `nextWeeklyRun(now, localHour, weekday, lastRunDate)` — the next `weekday`-at-`localHour`
  occurrence; skips to next week when today is already past `localHour`, or when today *is*
  `weekday` and this week's weekly digest already ran (`lastRunDate == localDate(now)`).
- On wake, `shouldRun()` is checked first (see "Leader gating" above), then a second guard-key
  check (against whichever cadence is about to fire) guards against a concurrent/pre-restart
  instance having already generated it while this instance slept.
- A failed generation retries after `digestScheduleRetry` (30 minutes) within the same day, rather
  than waiting until tomorrow.
- `digestEnabled`/`digestHour` read the config pointers (`Digest.Enabled *bool`,
  `Digest.LocalHour *int`) with the documented defaults (enabled; hour 7 — "a morning digest, not
  a midnight one"). `weeklyDigestEnabled` reads `Digest.WeeklyEnabled *bool` (default **off**).
  `digestWeekday` reads `Digest.Weekday` (`0`=Sunday…`6`=Saturday; out-of-range or unset defaults
  to `time.Monday`).
- `s.digests.Generate(ctx, kind, 0)` is called with `kind` = `"daily"` or `"weekly"` — the weekly
  call is what makes `DigestService.Generate` use the fixed 168h window regardless of the
  configured `digest.windowHours` (see `services/agent_digest.go.md`).

## Notes

- Disabled (both `daily` and `weekly` false): the loop idles, re-checking hourly —
  belt-and-braces, since a settings change only takes effect after a restart anyway
  (`services/settings_apply.go`).
- `sleepCtx(ctx, d)` sleeps for `d`, returning `false` (loop exits) if `ctx` is cancelled first.
- `lastDate(ctx, key)`/`setLastDate(ctx, key, date)` are the shared guard-key read/write
  (formerly `lastRunDate`/`setLastRunDate`, generalized to take either guard key) — both no-op
  when `s.settings` is nil.
- See `services/agent_digest.go.md` for what `Generate("daily"|"weekly", 0)` does, and `app.go`'s
  wiring (right after `apis.NewAgentApi`) for the actor id `0` convention (scheduler-generated).
