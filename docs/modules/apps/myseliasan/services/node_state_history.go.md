# Module: apps/myseliasan/services/node_state_history.go

## Purpose

Records what each node's liveness DID, so availability can be reported over a past window
rather than only observed in the present (flagship hardening plan W2-2, F-08). Writes
`entities/node_state_event.go.md` and `entities/fleet_monitor_gap.go.md`; read back by
`services/node_availability.go.md`.

Deliberately NOT part of `INodeRegistry`. The registry answers "what is the fleet now" and is
consulted on every page load; this answers "what was the fleet" and is consulted when
somebody generates a report. Keeping them apart is also what lets the registry's existing
tests keep their in-memory repos with no history at all.

## `INodeStateHistory`

| Method | Notes |
|---|---|
| `Observe(ctx, nodeID, state, reason, at)` | Reports a node's CURRENT state. Writes only when it differs from the last recorded state, or when the node has no history yet. |
| `Forget(ctx, nodeID)` | Deletes a node's history. Called on RELEASE. |
| `NoteSweep(ctx, now, maxGap)` | Stamps the monitoring watermark and records a gap when the previous stamp is older than `maxGap`. Called once per sweep, BEFORE reconciling. |
| `Prune(ctx, now)` | Drops aged events and gaps. Self-throttling to once a day. |
| `Events(ctx, nodeID, from, to)` | One node's transitions overlapping the window, INCLUDING the last event before `from`. |
| `Gaps(ctx, from, to)` | Monitoring gaps overlapping the window. |

## The transition test lives here, not in the callers

`Observe` is called unconditionally by every path that changes a node's status — four call
sites in three files. Putting the "did anything actually change?" test in the recorder rather
than in each caller is why they can all be one line. It is also why a node that has not
changed state in a year still gets its first row: the caller cannot know it is the first
observation, and the recorder can.

A first observation is written as `reason: baseline` with an empty `PrevState` **whatever the
caller said**. A row labelled `heartbeat` that is really the start of the record would be
indistinguishable from an ordinary transition, and an upgraded fleet would look as though it
had been measured all along.

## Concurrency

`mu` serialises the read-modify-write in `Observe`. The heartbeat sweep is leader-gated and
therefore single, but `AcceptControlConn` runs on the control server's accept path and
`MarkSelfDropped` on an HTTP handler, so two goroutines genuinely can observe the same node at
the same moment. Without the lock both read "last state = lost", both decide it is a
transition, and the node gains two identical recoveries — which is not merely untidy: the
second ends a zero-length span and the outage count is wrong for the rest of the year.

`last` caches the newest recorded state per node so a steady fleet does not query once per
node per sweep. It is populated lazily from the database, so a restart or a leadership
handover simply re-reads it — a restart must NOT re-baseline the whole fleet, or a restart
loop turns the history into a log of restarts (`TestObserveRereadsStateAfterARestart`).

## Retention: a node's newest event is never pruned

This is the whole subtlety of pruning this table. A node online without interruption for two
years holds exactly ONE row, and a plain "delete everything older than the cutoff" erases it —
turning the best-behaved appliance in the fleet into one with no history, reported as
unmonitored until it next changes state, which, being healthy, it does not. The row is not
stale data; it is the only thing asserting the node is up at all.

So `Prune` fetches the aged rows, looks up each affected node's newest row, and deletes
everything except that. Retention is 400 days — a year plus slack, so a twelve-month SLA
report always has a complete year behind it. Throttled to once a day; the sweep calls it every
pass so retention does not depend on anybody remembering to schedule it.

## Query cruxes

- **`Events` returns the last event BEFORE the window.** Without it, a node that went down in
  March and stayed down reports April as "no data" — the worst month reads as the emptiest
  one, which is the single most expensive way this feature could be wrong.
- **`Gaps` matches on OVERLAP, not containment.** A gap that started before the window and
  ended inside it is exactly the case that matters (the control plane was down over midnight
  on the first of the month); a containment filter drops it and the month reports as fully
  observed.

Both are covered by tests that run against a filter-honouring fake repo (`queryRepo` in
`node_state_history_test.go`) rather than the package's existing `memRepo`, which ignores
filters, sorters and limits — against that fake these tests would pass just as happily with
the filters removed.

## Related

- `services/node_registry.go.md` — the four call sites that report transitions.
- `services/node_availability.go.md` — the arithmetic over these rows.
- `services/backup.go.md` — both tables ride in the `fleet` backup section.
