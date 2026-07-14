# Module: apps/myiotsan/services/deadband.go

## Purpose

The deadband gate — the single mechanism that makes SQLite a viable telemetry store. 100
devices x 10 keys at 1 Hz is 1,000 rows/second, which SQLite will not absorb short of a
different database. But almost none of those samples SAY anything: a room is 21.4 degrees for
an hour, a door is shut all night. The gate persists a sample only when it actually MOVES.

This is measured, not theoretical: on a live appliance, 20 devices publishing ~30,000 samples
in under a second produced 540 written rows — 98.2% suppressed, zero dropped. See
`docs/MYIOTSAN_PLAN.md` §9.

## Key Type: DeadbandGate

```go
func NewDeadbandGate() *DeadbandGate
func (g *DeadbandGate) Admit(deviceId int64, key string, rule GateRule, num float64, str string, nowMs int64) bool
func (g *DeadbandGate) Forget(deviceId int64)
func (g *DeadbandGate) Size() int
```

In-memory, per-process, mutex-guarded map of `gateKey{deviceId, key} -> gateState`. Losing it
on restart costs exactly one extra row per series (the first sample after boot passes as
"first seen") — a far better trade than a database read on every incoming packet.

`Admit` decides via three gates, tried in order:

1. **First sample for the series** — always admitted; there is nothing to compare against.
2. **Heartbeat elapsed** (`rule.HeartbeatSeconds`) — admitted even if unmoved, so a flat line
   proves the device is alive and a chart has a point to draw.
3. **String key** — admitted on inequality (no magnitude to speak of).
4. **Numeric, `Deadband <= 0`** — admitted on any distinct value (still suppresses an identical
   repeat — a device republishing unchanged state every second will send one anyway).
5. **Numeric, `Deadband > 0`** — admitted when `abs(num - prev) >= Deadband`.

Every admitted sample re-baselines the stored value, so the deadband measures against the LAST
STORED value, not the series' original value — a slow drift that never crosses the deadband in
one step is still captured roughly every `Deadband` of cumulative travel.

`Forget(deviceId)` drops a device's whole series set — called on device delete/re-provision so
a future device reusing the id does not get compared against stale baselines.

## Key Type: GateRule

The part of a `TelemetryKey` the gate needs: `Deadband`, `HeartbeatSeconds`, `Numeric`
(distinguishes magnitude comparison from equality comparison).

## Notes

- `nowMs` is passed in rather than read from the clock, so this is testable and a batch of
  samples from one payload share a timestamp.
- Covered by `apps/myiotsan/services/deadband_test.go` (`deadband_test.go.md`).
- Consumed by `services.Ingest.Handle` on the hot ingest path
  (`apps/myiotsan/services/ingest.go.md`) — a suppressed sample still reaches the rule engine;
  the gate only decides whether it is WRITTEN.
- `Size()` backs the `series` field of `services.IngestStats`
  (`GET /api/devices/stats`) — the one unbounded structure in the ingest path, worth watching.
