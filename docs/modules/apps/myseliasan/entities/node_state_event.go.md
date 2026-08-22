# Module: apps/myseliasan/entities/node_state_event.go

## Purpose

`NodeStateEvent` — one recorded TRANSITION of a node's liveness. The fleet's memory of what
happened, as opposed to `ManagedNode.Status`, which only remembers what is happening now.

This is the data model behind node state history + SLA reporting (flagship hardening plan
W2-2, F-08). The control plane could always answer "is this node up?" and could never answer
"was it up last month?" — `services/reports.go` said so in its own footnote ("historical
uptime is not yet tracked"). A customer with an SLA does not ask the first question.

The only other record of an outage was a notification, and a notification is the wrong
instrument: it is retained on a rolling window, deduplicated, and edge-triggered, so it is a
record of ALERTS, not of STATE, and cannot be summed into an availability figure.

## States

`NodeStateOnline` / `NodeStateLost` / `NodeStateSelfDropped` — deliberately the SAME strings
`ManagedNode.Status` carries. History is a log of that field's transitions, not a parallel
vocabulary, so a state on the Nodes page and a state in an SLA report can never mean two
different things.

## Reasons

`baseline` / `heartbeat` / `control-channel` / `enroll` / `adopt` / `self-drop`. Carried for
diagnosis only — the availability maths never branches on them — so a reason written by a
future build is harmless.

## Fields

| Field | Notes |
|---|---|
| `Id` | Auto-increment primary key. |
| `NodeId` | `ManagedNode.NodeId`, **not** the numeric id: it is what survives a backup/restore and what the node asserts on the wire. The corollary is that releasing a node must delete its history — see `INodeStateHistory.Forget`. |
| `State` | One of the `NodeState*` values — what the node became at `At`. |
| `PrevState` | What it was before; empty on a baseline row. Recorded so a reader sees a transition without fetching the neighbouring row. |
| `At` | The unix second the state **began**, which is not always the second it was noticed — see below. |
| `Reason` | One of the `NodeStateReason*` values. |

## `At`: when the state began, not when it was noticed

This is the field with a decision in it, and the live bench found the first version of it
wrong.

The grace window means the sweep that declares a node lost runs up to three heartbeat
intervals (at least 90 seconds) after the node actually went quiet. Stamping that transition
with the sweep clock discards the whole window from **every** outage: the bench measured a
94-second outage recorded as 10 seconds. That is a published availability figure
under-stating downtime by a fixed amount per incident, always in the vendor's favour.

So a lost transition is dated to `ManagedNode.LastSeenAt` — the last moment there WAS
contact. That is safe because `LastSeenAt` is stamped by the control plane's own clock on
every path that sets it (`Heartbeat`, `AcceptControlConn`, `Enroll`) and never by the node,
so there is no remote skew to import. It is clamped both ways: a node never seen, or one
whose stamp is somehow in the future, falls back to the sweep clock rather than producing an
event that sorts before its own predecessor.

Every other transition is dated as observed, because for those the observation IS the event.

## Volume

Append-only, one row per CHANGE. A node up for a year writes one row, so the table stays
small on a healthy fleet and grows only where there is something to report. Retention is 400
days (`services/node_state_history.go.md`), and a node's newest row is never pruned however
old it is.

## Related

- `services/node_state_history.go.md` — what writes and reads these rows.
- `services/node_availability.go.md` — what turns them into a percentage.
- `entities/fleet_monitor_gap.go.md` — the record of when nothing was watching.
