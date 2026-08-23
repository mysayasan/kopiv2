# Module: apps/myseliasan/services/replay_horizon.go

## Purpose

`ReplayHorizonMonitor` — watches the clock on a promise that had nobody watching it (flagship
hardening plan W2-6, F-11). A node's events are delivered LIVE up the control channel; when the
channel is down they are not queued anywhere, they are recovered on reconnect by replaying the
node's stored notifications from the last `notifReplayWindow` (72h, `app.go`'s
`replayNodeNotifications`). That is the right design — no queue on the appliance, no new disk
pressure — but it has an expiry date: past the replay window a disconnect stops being
recoverable, and the fleet screens went on saying a node was "lost" in exactly the same tone at
hour 2 and at hour 90, with the moment the guarantee lapsed passing unremarked. This monitor warns
BEFORE that point, and again when it passes — a warning that arrives once the events are already
unrecoverable is an obituary, not an alert. See `apis/replay_horizon_api.go.md` for the HTTP
surface and `services/metrics.go.md` for `MetricReplayHorizonTotal`.

## Responsibilities

- `ReplayHorizonState` constants: `ok` (connected, or away less than the warn fraction of the
  window), `approaching` (far enough in that events will soon start falling out of reach), and
  `lapsed` (past the window — whatever the node raised before the horizon can never be replayed).
- `replayWarnFraction = 2/3` — how far into the window a disconnect gets before the first warning.
  On a 72h window that is a full day of notice, while being late enough that a node down over a
  weekend does not page anyone on Friday evening.
- `NewReplayHorizonMonitor(nodes replayHorizonNodeLister, connected func(nodeID string) bool,
  window time.Duration, notify func(nodeID, nodeName, state, detail string)) *ReplayHorizonMonitor`
  — `nodes` is a narrow `List(ctx) ([]*entities.ManagedNode, error)` view of `INodeRegistry`;
  `connected` is normally `ControlServer.IsConnected`; `notify` may be `nil` (then the monitor
  only reports, it never raises anything).
- `Sweep(ctx) (ReplayHorizonReport, error)` — evaluates every node once:
  - A connected node is always `ok` — it is forwarding live right now, nothing to recover.
  - A node with `LastSeenAt <= 0` (never seen) is left `ok` deliberately: there is no clock to
    measure against, and inventing one would either cry wolf on a node adopted a minute ago or
    stay silent forever. A node that never connects is the liveness monitor's business, not this
    one's.
  - Otherwise `AwaySeconds = now - LastSeenAt` is compared against `windowSeconds` (→ `lapsed`,
    `AwaySeconds >= windowSeconds`) and `warnAfter = windowSeconds * replayWarnFraction` (→
    `approaching`). A `lapsed` row's `UnrecoverableBefore` is `now - windowSeconds` — an ABSOLUTE
    timestamp on purpose: "events before 03:14 on Tuesday are gone" is actionable, "72 hours" is
    arithmetic homework.
  - `Last()` returns the most recent sweep without re-walking the fleet, for the API to serve
    cheaply.
- `raise(row)` — notifies once per state TRANSITION, not once per sweep: `warned map[nodeID]state`
  remembers what a node was last warned about, so a node sitting in `approaching` for a day
  produces ONE warning rather than one every 15 minutes. A monitor that repeats itself trains
  people to filter it out, and then the escalation to `lapsed` gets filtered out with it. A node
  that recovers (or drops out of the fleet) has its `warned` entry cleared at the end of `Sweep`,
  so a later disconnect is warned about afresh.
- `RegisterScheduler(start func(name string, interval time.Duration, run func(context.Context)
  error))` — helper for a periodic-task registrar; `app.go` instead drives the sweep via its own
  `leaderTicker` (see below), so this is available but not the path actually wired.

## Notes

- Wired from `app.go`'s `RegisterAppRoutes`, right after `NewNodeProxyApi`:
  `services.NewReplayHorizonMonitor(registry, controlServer.IsConnected, notifReplayWindow, notify)`
  where `notify` publishes a `notification.Notification` (category `health.check`, `Warning` for
  `approaching`, `Critical` for `lapsed`, `Source: "node:<id>"`) and increments
  `MetricReplayHorizonTotal{state}`. A `leaderTicker(bgCtx, deps.Leader, 15*time.Minute, ...)`
  drives the periodic sweep — **leader-gated** (deployment mode / Phase 1 multi-instance safety)
  so N instances don't each raise the same transition notification. `apis.NewReplayHorizonApi`
  is registered right after, over the same `*ReplayHorizonMonitor`. See `app/app.go.md`.
- Deliberately reuses `notifReplayWindow`, the same constant `replayNodeNotifications` pulls
  against — the monitor and the thing it is warning about the expiry of must always agree on what
  the window is, so this is a shared constant, not a second copy.
- Covered by `replay_horizon_test.go`.
