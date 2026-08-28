# Module: apps/mypintusan/services/cache_clock.go

## Purpose

Answers the one question `Decide()`'s GATE 10 asks and, until this change, nothing on this
appliance could answer: how long has this controller been deciding without contact with an
authority over its access data? `ControllerConfig.CacheAge` (`services/controller.go.md`) has
always been declared and `Decide()` has always compared it against a door's offline TTL — but it
was never assigned anywhere outside a unit test, so `Snapshot.CacheAge` was always zero on a real
install, `s.CacheAge > ttl` was always false, and the reason code `offline-cache-expired` — the
one the whole offline design turns on — could not be produced on any installation. The first live
bench of offline mode (`tools/fleetbench/bench_pintusan_offline.py`) measured it directly: a door
20 seconds past a 2-second TTL still granted. This is the same shape as the three unreachable
alarms `services/controller.go.md`'s bench Notes describe (#220) — the gate was right, the reason
code was right, the translations were right, and the number the comparison needed was never
computed by anything.

## Key Type: CacheClock

```go
type CacheClock struct {
    repo settingsRowRepo
    now  func() time.Time

    mu      sync.Mutex
    last    time.Time // most recent contact; zero means "not yet loaded from the database"
    written time.Time // what `last` was when last persisted (throttle bookkeeping)
}
```

`settingsRowRepo` is a narrow three-method interface (`GetByUnique`/`Create`/`UpdateById`) over
the shared `runtime_setting` table, rather than the full sixteen-method generic repo — a test that
had to implement all sixteen to pin a timestamp is a test nobody writes. `NewCacheClock(repo)`
builds it; `repo` is normally `dbsql.NewGenericRepo[sharedentities.RuntimeSetting](deps.Db)`,
wired in `app/app.go.md`'s `RegisterAppRoutes`.

## What counts as contact

Two signals reset the clock, and both are wired outside this file:

- **The fleet control channel connecting to its control plane.** A node whose parent can reach it
  is a node a revocation can reach. Wired via `domain/shared/fleetnode/control_channel.go.md`'s
  `ControlChannelManager.SetOnContact`, called from `app/wire_fleet.go.md`'s `buildFleet`.
- **An administrative change to the access data on this appliance itself** — a holder, credential,
  group, schedule, grant or door. An operator standing at the controller changing the rules is the
  most direct source of truth there is. Wired via `app/app.go.md`'s `ruleChangeTouch` middleware,
  which calls `Touch` after any 2xx `POST`/`PUT`/`PATCH`/`DELETE` under `accessRulePaths`.

A controller nobody can reach through either path is the case the TTL exists for; the second signal
is what keeps that from also catching a controller that IS being actively administered.

## Responsibilities

- `Touch(ctx)` — records contact now. Nil-safe (a `Controller` assembled without a clock — every
  unit test, and any future embedding — simply has no staleness to report). Writes are throttled
  to once a minute (`cacheClockPersistEvery`): the in-memory value (`c.last`) is always current;
  only the persisted row lags, which is what keeps a rule edit or a control-channel frame off the
  write path of every request.
- `Age(ctx)` — how long since the last contact. Two directions are deliberately chosen for safety
  in opposite ways: an appliance with **no record at all** (never yet administered — commissioning)
  returns `0`, the fail-SAFE reading, rather than infinity, which would brick the commissioning
  visit; a clock that has gone **backwards** (NTP stepping an embedded box with no RTC) is also
  read as `0` and re-anchored, rather than treated as either fresh or expired evidence — a negative
  age is a measurement fault, and denying a whole site on it would be the wrong way to be wrong.
  Both paths call `Touch` internally, so the very next decision is measured against something real.
- `AgeFunc()` — returns the `func() time.Duration` accessor `ControllerConfig.CacheAge` wants,
  bound to `context.Background()` rather than the caller's request context: the decision path's
  context is the badge's, which is torn down long before the age is next asked for, and must not
  abandon a pending persist.
- `load(ctx)` / `persist(ctx, at)` — the database half. `load` reads the `"access.lastContact"` row
  via `GetByUnique(ctx, "", "key", cacheClockKey)` — the same `GetByUnique`-against-a-real-`ukey`
  pattern `services/runtime_settings.go.md` documents, for the same reason: a key group matching no
  declared `ukey` falls through to an unfiltered select and returns the first row in the table.
  `persist` creates the row if absent, otherwise updates it.
- **Persistence is not an optimisation.** A door controller rebooting while cut off from its
  control plane is precisely the scenario the TTL exists for; an in-memory-only clock would hand a
  replica that has been unreachable for a week a fresh 72 hours on every power cycle, turning the
  TTL into a formality anybody can reset by pulling the plug.

## Notes

- Wired in `app/app.go.md`'s `RegisterAppRoutes` (built before the runtime, before the settings
  service's `OnChange` listener, and before `ruleChangeTouch` is registered) and in
  `app/wire_fleet.go.md`'s `buildFleet` (via `control.SetOnContact`). Threaded into every
  `Controller` this app builds via `app/runtime.go.md`'s `runtime.cache` and
  `ControllerConfig.CacheAge: r.cache.AgeFunc()`.
- Covered by `services/cache_clock_test.go`: age growth and reset on contact, restart survival
  (two `CacheClock`s over the same fake repo), a backwards-clock re-anchor, the virgin-appliance
  zero-age start, the write throttle (50 touches inside a minute produce ≤2 writes), and a nil
  `*CacheClock` being safe to call.
- Live-benched by `tools/fleetbench/bench_pintusan_offline.py` (19/19; 12/19 against the unfixed
  app) — see `services/controller.go.md`'s "What the third live bench confirmed" and
  `docs/MYPINTUSAN_OSDP_PLAN.md` §11.
