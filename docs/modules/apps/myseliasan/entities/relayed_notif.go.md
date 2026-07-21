# Module: apps/myseliasan/entities/relayed_notif.go

## Purpose

`RelayedNotif` is a short-lived dedup ledger row for a node-originated notification the control
plane has already ingested. It exists so the reconnect replay (`apps/myseliasan/app/app.go.md`'s
"Replay on reconnect") never re-publishes a notification already delivered — whether that
delivery happened on the live control-channel push or an earlier replay pull.

## Fields

| Field | Notes |
|---|---|
| `Id` | Auto-increment primary key. |
| `DedupKey` | `"<nodeId>|<originId>"`, unique (`ukey`) — one row per distinct node event, regardless of which path (live push or replay pull) recorded it first. |
| `NodeId` | The owning node's id, also folded into `DedupKey`. |
| `CreatedAt` | The **origin** notification's unix time (not the marker's own insert time), so pruning can compare it against the replay window. |

## Notes

- Written and read exclusively through `services.RelayDedup` (`services/relay_dedup.go.md`) — this
  entity has no API of its own and is never surfaced to an operator.
- Rows are pruned once they fall outside twice the replay window (`2 * notifReplayWindow`, 144h):
  a bounded pull can never reach back that far, so an older marker is dead weight, not history.
  This is a dedup ledger, not an audit trail — see `entities/audit_log.go.md` for the durable
  record of sensitive actions.
- Registered for DB bootstrap in `apps/myseliasan/app/app.go`'s `Entities()`; created by the
  auto-migrator like any other new table (no explicit column migration needed, since it is a
  brand-new table rather than a column added to an existing one).
