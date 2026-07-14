# Module: apps/myseliasan/services/metrics.go

## Purpose

myseliasan's runtime metrics catalog and fleet sampler. Same rule as `myiotsan/services/metrics.go.md`:
**instrument what FAILS SILENTLY.** A control plane is a RELAY — it holds no cameras and no
sensors of its own — so its failures are failures of the fleet, and they are the quietest kind. A
node drops off the control channel and the UI simply shows one fewer node, which looks identical
to a node an operator released on purpose; a node's certificate creeps toward expiry with no
symptom at all until the day it expires and the node cannot re-enroll. The app itself keeps
answering perfectly while the fleet quietly degrades — exactly the situation a scrape has to
surface.

## Metrics

- `MetricNodesConnected` = `myseliasan_nodes_connected` — nodes currently holding a live control
  channel; the fleet that is actually reachable **right now**, as distinct from adopted.
- `MetricNodesAdopted` = `myseliasan_nodes_adopted` — nodes adopted in total. The gap between
  adopted and connected is the fleet that is supposed to be there and is not.
- `MetricControlChannelUp` = `myseliasan_control_channel_up` — `1` while the control-channel
  listener is serving, else `0`. If this is `0`, no node can reach the control plane and the whole
  fleet is about to look lost at once — the first thing to check when it does.
- `MetricFleetEventsTotal` = `myseliasan_fleet_events_total` ({kind=node_lost|node_recovered|
  cert_expiring}) — fleet-health transitions, incremented at the fleet-event sink. A burst of
  `node_lost` is a network partition or the control channel dying; a steady trickle of
  `cert_expiring` is enrollment quietly failing across the fleet.
- `MetricFleetRuleFiredTotal` = `myseliasan_fleet_rule_fired_total` ({severity}) — cross-domain
  correlation rules firing (`services/correlate.go`'s `fire`). Low-volume by nature (an intrusion
  is rare); a spike is either a real incident or a rule mistuned into crying wolf, and both are
  worth seeing.
- `DescribeMyseliasanMetrics(m telemetry.Metrics)` registers help text for all five; called once
  at startup (`app.go`).

## Sampling the fleet gauges

`RunFleetMetricsSampler(ctx, m, conns connectionSource, adoptedCount func(ctx) int, interval)`
reads `myseliasan_control_channel_up` and `myseliasan_nodes_connected` off the control server and
`myseliasan_nodes_adopted` off the caller-supplied `adoptedCount` closure, on a `10s` ticker
(`safego.Supervise`d as `myseliasan.metrics-sampler`). Sampled rather than instrumented
per-connection: connections come and go, but a scrape every ten seconds is plenty to watch a
fleet, and it keeps the control-channel accept path free of a metrics lock.

`connectionSource` is a narrow consumer-defined interface (`IsListening() bool`,
`ConnectedCount() int`) satisfied by `*services.ControlServer`. `adoptedCount` is a plain func
rather than an interface so `app.go` can pass a closure over `registry.List` directly, without a
shim type.

## Counted directly, not sampled

- `MetricFleetEventsTotal` is incremented at the fleet-event sink in `app.go` (inside
  `registry.SetFleetEventSink`'s closure), right after `publishFleetEvent` — see
  `app/app.go.md`'s `fleetEventKind` helper for the kind→label mapping.
- `MetricFleetRuleFiredTotal` is incremented inside `Correlator.fire` via `Correlator.SetMetrics`
  (a setter, not a constructor argument — see `services/correlate.go.md`, "so the ten existing
  correlator tests don't all grow a nil").

Both are rare, discrete events (not a hot path), so a direct labelled `Inc` is fine — no sampling
needed.

## Notes

- Wired from `app.go`'s `RegisterAppRoutes`: `services.DescribeMyseliasanMetrics(deps.Metrics)`
  before the fleet-event sink is registered, then `services.RunFleetMetricsSampler(bgCtx,
  deps.Metrics, controlServer, ..., 10*time.Second)` after the control server starts. See
  `app/app.go.md`.
- Before this file, myseliasan exposed zero series on `/metrics`; a live scrape confirmed it.
