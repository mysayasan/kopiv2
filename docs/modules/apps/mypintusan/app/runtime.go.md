# Module: apps/mypintusan/app/runtime.go

## Purpose

`runtime` owns the OSDP buses and their controllers, and — the reason this file exists — keeps
them alive. One `runtime` per process; one supervised goroutine per configured bus
(`services/runtime_settings.go.md`'s `services.BusSettings`). It also holds the site's lockdown
state outside any single controller, and is the seam `apis.NewDoorApi`/`apis.NewLockdownApi`
(`apis/doors.go.md`) call through for an operator-initiated unlock or a lockdown toggle.

`cfg` was `*pintuconfig.Config` (`config/config.go.md`) and `BusConfig`/`ReaderConfig` throughout;
it is now `services.AccessSettings` and `services.BusSettings`/`ReaderSettings`
(`services/runtime_settings.go.md`). `app/app.go.md`'s `RegisterAppRoutes` builds this value from
the **database-backed** access settings, not `config.json` directly — `config.json` only seeds the
first boot. Nothing about the bus-supervision behaviour itself changed; only where the values that
drive it come from.

## Key Type: runtime

```go
type runtime struct {
    deps    apphost.Dependencies
    cfg     services.AccessSettings
    store   services.Store
    alarms  services.Alarmer
    strikes services.StrikeResolver
    // decisions receives every access decision for the notification feed (and, through it, the
    // fleet uplink). Nil when nothing consumes decisions.
    decisions func(ctx context.Context, ev appentities.AccessEvent)
    // cache measures how stale this controller's replica is. See services.CacheClock: without it
    // Decide()'s TTL comparison has no number to compare and can never deny.
    cache *services.CacheClock

    cancel context.CancelFunc

    // offline is the LIVE answer to "is this controller serving from a cached replica?", held as
    // an atomic.Bool rather than copied into each controller — controllers are rebuilt on every
    // bus reconnect, and a value captured at process start cannot follow a setting an operator
    // changes afterwards. Before this the flag was read once at boot: turning offline mode on from
    // the settings API persisted the change, returned 200, read back as true, and every door
    // carried on deciding as though online.
    offline atomic.Bool

    // location is the LIVE site timezone, atomic for the same reason offline and lockdown are.
    // It is the first question the setup wizard asks, on an already-running controller, so
    // "takes effect at the next restart" meant every fresh install decided its schedules and
    // holidays in the seeded zone (UTC on an appliance) until somebody power-cycled it. Handed to
    // each controller as the SiteLocation method value, so a correction reaches the next badge.
    location atomic.Pointer[time.Location]

    mu       sync.RWMutex
    lockdown bool
    live     map[string]*services.Controller // bus port -> current controller
}
```

`lockdown` is held **here**, not only inside a `Controller`, because a controller is rebuilt on
every reconnect — keeping the flag on the runtime is what stops unplugging a USB adapter from
quietly lifting a lockdown. `live` maps a bus port to its current controller; the entry is
replaced wholesale on each reconnect. `offline` follows the identical reasoning, one level up: a
`Controller` reads `ControllerConfig.Offline` as a `func() bool` bound to `runtime.Offline`
(below), not a value baked in at `runBus` time.

## Responsibilities

- `newRuntime(deps, cfg, loc, store, alarms, decisions, strikes, cache)` — gained an eighth
  parameter, `cache *services.CacheClock` (`services/cache_clock.go.md`), threaded straight
  through into every `Controller`'s `ControllerConfig.CacheAge`. `offline` is deliberately **not**
  seeded here; the caller (`app/app.go.md`'s `RegisterAppRoutes`) applies it through `SetOffline`
  so a site whose `config.json` already has `access.offline: true` on first boot gets the
  degraded-mode alert like any other transition.
- `SetOffline(ctx, on)` — puts the site into, or takes it out of, cached-replica mode and raises
  (or clears) `services.AlarmDegraded`. A no-op if the value did not change (`r.offline.Swap(on) ==
  on`), so re-saving the same settings does not re-alert. **The alert is the point**: a cached
  grant is indistinguishable from a live one at the door, on the activity screen and in the access
  log's decision column — the only difference is a boolean nobody reads — and
  `docs/MYPINTUSAN_DATA_MODEL.md` §2 has always said a controller running degraded "raises a
  degraded-mode alert immediately." Before this method existed, nothing raised one; the first
  visible sign that a site was degraded was a door refusing a valid badge once its TTL ran out,
  hours or days later, reported by whoever was standing outside it.
- `Offline()` — reports the live cached-replica state (`r.offline.Load()`); this is the function
  bound into `ControllerConfig.Offline` for every controller `runBus` builds.
- `SiteLocation()` — the live site timezone, bound into `ControllerConfig.Location` for every
  controller `runBus` builds and therefore consulted on every badge. Falls back to the host's
  zone, never silently to UTC.
- `SetLocation(loc)` — swaps the zone under the running controllers, logging the change (a site
  timezone moving under a live access-control system is worth a journal line).
- `ApplySettings(ctx, s)` — the listener registered via `settings.OnChange` in `app/app.go.md`.
  Carries a settings edit into the RUNNING controllers, and is explicit about which parts of the
  edit it could NOT carry: `s.Offline` and the SITE TIMEZONE take effect live
  (`r.SetOffline` / `r.SetLocation`). The bus list, reader keys and tick interval are read while
  building a bus supervisor and a controller, and re-reading them live would mean tearing down
  every segment on the site mid-save — on a door controller, every door going dead for the length
  of a reconnect because somebody renamed a reader. The Settings screen's restart note
  (`settings.restartNote`) names exactly those.

  The timezone was on that list and did not belong there: it is a value the decision path reads,
  not a resource anything holds open, and it is the setting a site is most likely to get wrong at
  installation, since the first-run wizard asks for it before any door exists. Swapping it
  interrupts no door and no session. A zone that will not load is REFUSED and the previous one
  kept (`validateAccessSettings` already turns one away at the API, so reaching here with a bad
  zone means the database was edited underneath the app — and silently moving every schedule to
  UTC would be the worst possible answer to that).
- `start(ctx)` — spawns `superviseBus` (via `safego.Go`) for every configured bus whose `Port`
  is non-empty. Zero configured buses is not an error — logged, not failed — since a fresh
  install has none until somebody wires a reader; the API still comes up.
- `superviseBus(ctx, cfg)` — **the loop that is the difference between a door controller and a
  demo.** Runs `runBus` in a loop, re-dialling with **1s → 30s exponential backoff** whenever it
  returns an error, until `ctx` is cancelled. Discovered by booting the app against
  `tools/osdp-sim` and then restarting the simulator: without this loop, the app never
  reconnected — events simply stopped, with nothing in the logs saying why — and no unit test
  caught it, because the existing tests run over an in-memory pipe that is never torn down
  mid-run (see `infra/access/osdp/bus.go.md`'s `failPort` for the driver-side half of this fix).
  - **The backoff resets after a session that ran for 20 s or more.** Without that reset it
    doubled over the life of the *process* rather than over the current outage, so a site whose
    gateway reboots nightly reached the 30 s cap after about five events and stayed there for
    good — every subsequent knock leaving the segment's doors dead for half a minute after the
    cable was already back, which is the exact delay the cap exists to prevent. Measured live at
    28.5 s for the sixth knock against 5.2 s for the same knock once the reset was in
    (`MYPINTUSAN_OSDP_PLAN.md` §12).
- `runBus(ctx, cfg)` — dials the transport (`dialBus`), builds a fresh `osdp.Bus` and enrols
  every configured reader (refusing, per-reader rather than per-bus, a `RequireSecureChannel`
  reader with a missing or malformed SCBK — a mistyped key must never silently become a
  cleartext session on a door configured to require encryption), builds a fresh
  `services.Controller` (`services.BusActuator{Bus: bus, Resolve: r.strikes}`, with
  `ControllerConfig.Decisions: r.decisions`, `ControllerConfig.Offline: r.Offline` (a live
  question, not the config value captured at boot — see `services/controller.go.md`'s `offline()`)
  and `ControllerConfig.CacheAge: r.cache.AgeFunc()` — see `services/controller.go.md`'s
  `DoorStrike`/`StrikeResolver`/`Decisions`), records it in `r.live`, **carries
  the site's current lockdown state into the new session before polling starts** (`ctrl.
  SetLockdown` ahead of `ctrl.Run`, closing the window in which a reconnected bus could honour a
  badge the site is currently refusing), then runs the controller and the bus concurrently until
  either ends.
  - A fresh `Bus` and `Controller` are built on **every** attempt, deliberately: `Bus` closes its
    port on exit, and the per-PD sequence number and Secure Channel session belong to the dead
    session — resuming an old session against a possibly power-cycled or swapped reader is
    exactly the substitution Secure Channel exists to prevent.
  - **`Bus` closes the port in `Run`'s defer and nowhere else**, so every path that returns
    without reaching `Run` closes the transport itself. The `enrolled == 0` path did not, and
    `superviseBus` retries for ever: one `requireSecureChannel` with no key, or one mistyped
    SCBK, on a single-reader segment leaked a connection per re-dial to a gateway that commonly
    accepts one to four TCP clients.
  - **When `Bus.Run` returns, `ctrl.BusDown` is called** with the addresses that were enrolled,
    so the segment's loss reaches the alarm feed and the readers list before the re-dial loop
    takes over. Skipped on ordinary shutdown — the app stopping is not a door alarm — and gated
    by `announceBusDown` so ONE outage produces ONE alarm however many re-dials fail inside it: a
    gateway that is rebooting accepts the connection and drops it again, repeatedly, and paging on
    each of those would deliver the alarm-fatigue failure `services/alarm.go` argues against
    through the very alarm added to prevent silence. A session lasting `busHealthyRun` (20 s) —
    the same threshold that resets the backoff — ends the outage and re-arms it. See
    `services/controller.go.md`'s `BusDown`.
- `stop()` — cancels the runtime's root context, which propagates to every bus supervisor.
- `SetLockdown(ctx, on)` / `Lockdown()` — set/read the site-wide flag under `mu`, and fan a set
  out to every currently-live controller. `Lockdown()` reads the **runtime's** flag, not a
  controller's, deliberately: a site whose buses are all disconnected is still in lockdown, and
  reporting "not locked down" because nothing is live would be a dangerous lie to put in front
  of an operator.
- `Unlock(ctx, door, actor, actorName)` — the operator-unlock seam `apis.doorApi.unlock` calls.
  Tries every currently-live controller's `OperatorUnlock` in turn, treating
  `services.IsUnknownDoor` as "try the next bus" and anything else as a hard failure; returns an
  error if no bus is connected or no connected bus owns the door.
- `dialBus(ctx, port)` — `tcp://host:port` dials `osdp.DialTCP`; anything else is refused with an
  explicit error. **Serial is not implemented** (`MYPINTUSAN_OSDP_PLAN.md` §5 build-order step
  5) — only the simulator/TCP-gateway form works today.

## Notes

- Not exported outside `app`; `apis.Unlocker` (`apis/doors.go.md`) is the narrow interface the
  HTTP layer actually depends on (`Unlock`/`SetLockdown`/`Lockdown`), so a handler cannot reach a
  bus or a controller directly.
- `newRuntime` gained a new parameter, `decisions func(ctx context.Context, ev appentities.AccessEvent)`,
  inserted between `alarms` and `strikes` — `apps/mypintusan/app/app.go.md`'s `RegisterAppRoutes`
  passes `alarms.Decision` (`services/alarm.go.md`), so every controller this runtime builds
  publishes badge decisions into the notification feed, which the fleet control channel then
  forwards to an adopted `myseliasan` (`app/wire_fleet.go.md`).
- The first live bench of offline mode found that turning `access.offline` on from the settings API
  persisted, returned `200`, read back `true`, and left every door deciding as though it were
  online — `ControllerConfig.Offline` was a plain `bool` copied out of `r.cfg` at `runBus` time,
  and `r.cfg` was never updated after boot. Fixed by making it `func() bool` bound to the new
  `runtime.offline atomic.Bool` (`SetOffline`/`Offline` above), reached live via
  `IAccessSettingsService.OnChange` (`services/runtime_settings.go.md`) →
  `runtime.ApplySettings` → `runtime.SetOffline`. See `docs/MYPINTUSAN_OSDP_PLAN.md` §11 and
  `tools/fleetbench/bench_pintusan_offline.py`.
- Both bugs this app shipped with were found by booting the binary, not by a unit test: the bus
  never reconnecting (fixed by this file's supervise loop plus `infra/access/osdp/bus.go.md`'s
  `failPort`), and `BusActuator` addressing the wrong number (fixed in
  `services/controller.go.md`/`services/store_sql.go.md`'s `StrikeFor`).
