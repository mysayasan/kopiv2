# Module: apps/myseliasan/entities/fleet_monitor_gap.go

## Purpose

`FleetMonitorGap` — a span during which NOTHING WAS WATCHING the fleet: the control plane was
down, mid-upgrade, or (in a cluster) between leaders for longer than a heartbeat grace
window.

This table is the difference between an availability report and a fiction.

Node state history is a log of observed TRANSITIONS, so a node that was online when the
control plane stopped is still "online" in the log while the control plane is dead. A naive
reader credits that whole outage to the node as uptime. The one period we can be certain we
know nothing about is the period we were not running — and it is also exactly the period
during which our own failure is most likely to have coincided with something else going
wrong.

So a gap is subtracted from the denominator and reported in its own right. The resulting
figure reads "available for 99.4% of the time we were watching, and we were watching 97% of
the month", which an operator can act on and a customer can audit. Silently reporting 99.4%
of a month a third of which was never observed is the sort of number that survives right up
until somebody checks it.

## Detected, not declared

The heartbeat sweep stamps a watermark every pass (`monitor.lastSweepAt` in
`control_setting`), and a sweep that finds the watermark older than the lost-grace window
writes the span it just discovered. That covers a crash, a `kill -9`, a host reboot and a
long upgrade identically, because none of them get to run shutdown code.

The first sweep after a fresh install (or a restore that dropped the watermark) claims
nothing: time before the first watermark is already unmonitored by construction, since no
node has any history before its first event either.

## Fields

| Field | Notes |
|---|---|
| `Id` | Auto-increment primary key. |
| `StartedAt` | The last sweep before monitoring stopped. |
| `EndedAt` | The first sweep after it resumed. The truth is somewhere inside that span, so the whole span is treated as unmonitored — erring toward "we do not know", never toward "it was up". |
| `Reason` | Free text for the operator; the maths never reads it. |

## Symmetry

Gap subtraction cuts both ways. A gap covering an outage removes the DOWNTIME too, and the
outage is not counted — otherwise the mechanism would become a way to launder downtime out of
the record. `TestTallyGapOverAnOutageRemovesTheDowntimeToo` pins that.

## Related

- `entities/node_state_event.go.md` — the transitions this qualifies.
- `services/node_state_history.go.md` — `NoteSweep`, which detects and writes these.
- `services/node_availability.go.md` — the subtraction itself.
