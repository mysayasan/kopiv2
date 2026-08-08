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
    deps     apphost.Dependencies
    cfg      services.AccessSettings
    location *time.Location
    store    services.Store
    alarms   services.Alarmer
    strikes  services.StrikeResolver
    // decisions receives every access decision for the notification feed (and, through it, the
    // fleet uplink). Nil when nothing consumes decisions.
    decisions func(ctx context.Context, ev appentities.AccessEvent)

    cancel context.CancelFunc

    mu       sync.RWMutex
    lockdown bool
    live     map[string]*services.Controller // bus port -> current controller
}
```

`lockdown` is held **here**, not only inside a `Controller`, because a controller is rebuilt on
every reconnect — keeping the flag on the runtime is what stops unplugging a USB adapter from
quietly lifting a lockdown. `live` maps a bus port to its current controller; the entry is
replaced wholesale on each reconnect.

## Responsibilities

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
- `runBus(ctx, cfg)` — dials the transport (`dialBus`), builds a fresh `osdp.Bus` and enrols
  every configured reader (refusing, per-reader rather than per-bus, a `RequireSecureChannel`
  reader with a missing or malformed SCBK — a mistyped key must never silently become a
  cleartext session on a door configured to require encryption), builds a fresh
  `services.Controller` (`services.BusActuator{Bus: bus, Resolve: r.strikes}`, with
  `ControllerConfig.Decisions: r.decisions` — see `services/controller.go.md`'s
  `DoorStrike`/`StrikeResolver`/`Decisions`), records it in `r.live`, **carries
  the site's current lockdown state into the new session before polling starts** (`ctrl.
  SetLockdown` ahead of `ctrl.Run`, closing the window in which a reconnected bus could honour a
  badge the site is currently refusing), then runs the controller and the bus concurrently until
  either ends.
  - A fresh `Bus` and `Controller` are built on **every** attempt, deliberately: `Bus` closes its
    port on exit, and the per-PD sequence number and Secure Channel session belong to the dead
    session — resuming an old session against a possibly power-cycled or swapped reader is
    exactly the substitution Secure Channel exists to prevent.
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
- Both bugs this app shipped with were found by booting the binary, not by a unit test: the bus
  never reconnecting (fixed by this file's supervise loop plus `infra/access/osdp/bus.go.md`'s
  `failPort`), and `BusActuator` addressing the wrong number (fixed in
  `services/controller.go.md`/`services/store_sql.go.md`'s `StrikeFor`).
