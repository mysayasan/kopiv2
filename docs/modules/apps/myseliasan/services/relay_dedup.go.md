# Module: apps/myseliasan/services/relay_dedup.go

## Purpose

`RelayDedup` deduplicates node-originated notifications the control plane ingests, so replaying a
node's missed notifications on reconnect (`app.go`'s `replayNodeNotifications`) never
double-inserts one already delivered live (or by an earlier replay). It keys on the node's stable
engine id (`infra/notification.Notification.ID`), which travels on **both** the live
control-channel push and the persisted row a replay pull returns (round-tripped through
`domain/notification.OriginIDKey`, see `domain/notification/store.go`).

## Constructor

`NewRelayDedup(db)` — builds the dedup ledger over the control plane's own database
(`dbsql.NewGenericRepo[entities.RelayedNotif](db)`). Wired once in `app.go` and shared by
`ingestNodeEvent`/`republishNodeNotification` (live path) and `replayNodeNotifications` (replay
path).

## Responsibilities

- `SeenOrRecord(ctx, nodeID, originID, createdAt) bool` — reports whether `(nodeID, originID)` has
  already been ingested. On a first sighting it records the marker (`entities.RelayedNotif`,
  `DedupKey = "<nodeID>|<originID>"`) and returns `false` (caller should ingest); on a repeat it
  returns `true` without inserting again. An empty `originID` (an older node build predating the
  origin-id feature) cannot be deduped and always returns `false` — best-effort: ingest it, since
  the worst case is a rare duplicate rather than a dropped event. `createdAt` of `0` is stamped
  with the current time so the marker still has a value to prune against.
- Per-node locking (`nodeLock`, a `map[string]*sync.Mutex` guarded by an outer `sync.Mutex`)
  serializes the check-then-record for one node, so a live push and a concurrent replay of the
  same event can't both observe "not seen yet" and publish it twice. Contention is low in
  practice: a node's events arrive on its one control connection plus, at most, its one replay
  goroutine per reconnect.
- `Prune(ctx, olderThan) (int, error)` — deletes every marker whose `CreatedAt` is before
  `olderThan` (unix seconds). Called from an hourly goroutine in `app.go` with
  `olderThan = now - 2*notifReplayWindow` — a windowed replay pull can never reach back further
  than the window, so an older marker can never be re-offered and is safe to drop.

## Notes

- Backed by `entities.RelayedNotif` (`entities/relayed_notif.go.md`) — a dedup ledger, not a
  durable audit record; unlike `IAuditService` (`services/audit.go.md`) rows are expected to be
  pruned.
- `GetByUnique(ctx, "", "dedup_key", key)` is the existence check; a hit (non-nil, no error) means
  already-seen. This is a distinct code path from the `GetByForeign`/`GetById` pitfalls documented
  elsewhere in the suite — `dedup_key` is a genuine unique index, not a foreign-key lookup.
