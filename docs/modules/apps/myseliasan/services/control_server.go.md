# Module: apps/myseliasan/services/control_server.go

## Purpose

Implements the parent side of the bi-directional parent↔node control channel. Accepts node-initiated WebSocket-over-mTLS connections, tracks live connections per node, and tunnels request/response frames.

## Key Type: ControlServer

Built via `NewControlServer(registry, port, onEvent, logf)`. Default port: `49533`.

### Responsibilities

- Accepts incoming node connections (nodes dial the parent, not the other way round) authenticated via fleet-CA mTLS.
- Maintains `conns map[nodeID → *control.Conn]` of live connections.
- `SendRequest(ctx, nodeID, req)` — tunnels a `control.Request` frame to the node and waits for the correlated `control.Response`. Returns `ErrNodeOffline` when the node has no live connection. Falls back to `controlRequestTimeout` (30s) when the caller's context has no deadline. If the node's control channel drops while the request is still in flight, the waiter is delivered a nil frame (via `failPending`) and `SendRequest` returns `ErrNodeDisconnected` immediately instead of blocking to the timeout.
- `IsConnected(nodeID string) bool` — reports whether `nodeID` currently holds a live control-channel connection. Exposed to `INodeRegistry.SetControlPresence` so the heartbeat reconciler can treat a live connection as the authoritative online signal before falling back to the mTLS poll.
- `IsListening() bool` — reports whether the control channel's serve loop (`Run`) is currently active, backed by an `atomic.Bool` set true for the duration of `srv.Run(ctx)`. Feeds `app.go`'s `ReadinessStatus` (advisory `controlChannel` field on `/api/ready`) — see that module's doc for the never-gates-ok contract.
- `ConnectedCount() int` — returns the number of nodes currently holding a live control connection (`len(conns)`, mutex-guarded). Feeds the advisory `connectedNodes` field on `/api/ready`.
- Receives node-pushed event frames and dispatches them to the optional `NodeEventHandler` callback (`onEvent`).
- `SetOnConnect(fn func(nodeID string))` — registers an optional callback invoked (in its own goroutine, off the read loop) the moment `handleConn` accepts a node's connection, after `add`/`ForgetRejected`. This is the reconnect hook `app.go` uses to replay any notification a node published while its control channel was down (`replayNodeNotifications`) — see `app.go.md`'s "Replay on reconnect" note. Set once at startup, before `Run`; nil is a no-op (no field to check at the call site besides the nil guard in `handleConn`).

### Rejected (stranded) connection tracking

`handleConn` refuses a connection whenever `registry.AcceptControlConn` errors (unknown node, or a revoked cert) — before this, that refusal was invisible beyond a log line, so a stranded node (paired, cert still valid, but no managed record on this side) would dial forever with no way for an operator to notice or stop it:

- `recordRejected(nodeID, remoteAddr, reason)` — called from `handleConn` right before closing a refused connection. Deduped by `nodeID`: a repeat (the node retrying in a loop) bumps `Count`/`LastSeen`/`RemoteAddr`/`Reason` on the existing entry rather than growing the map; a genuinely new id gets a fresh `RejectedNode{FirstSeen, LastSeen: now, Count: 1}`. Bounded at `maxRejected` (200) — past that, the stalest entry (oldest `LastSeen`) is evicted first, so a flood of distinct fabricated ids can't grow the map without limit.
- `Unrecognized() []RejectedNode` — snapshot of the tracked entries, newest (`LastSeen`) first. Backs `GET /api/nodes/unrecognized` (`apis/nodes.go.md`) via the `rejectTracker` seam.
- `ForgetRejected(nodeID)` — drops one entry. Called both when an operator explicitly dismisses/blocks a stranded node (`apis/nodes.go.md`'s `forgetNode`/`blockNode`), and automatically from `handleConn` the moment a node that WAS being refused connects cleanly — a node that starts working again is no longer a problem worth flagging, so any stale rejection for it is cleared as soon as `AcceptControlConn` succeeds.
- `RejectedNode` (`NodeID`, `Reason`, `RemoteAddr`, `FirstSeen`, `LastSeen`, `Count`, all JSON-tagged) is a value type returned by copy from `Unrecognized()`, so a caller can't mutate the server's internal tracking state.
- Covered by `control_server_test.go`: dedup + count bump on a repeat rejection, newest-first ordering, `ForgetRejected` removing one entry, `AcceptControlConn`'s revoked-checked-before-unknown ordering (a blocked-but-row-less node reports `ErrNodeRevoked`, not `ErrNodeUnknown`), and an end-to-end test that dials a real stranded node (valid enrolled cert, row removed underneath it) through the real TLS listener and confirms it lands in `Unrecognized()`.

### In-flight request tracking (`pendingReq` / `failPending`)

- `pending` is now keyed correlation-id → `pendingReq{ch, nodeID}` (previously just the raw waiter channel) so a disconnect can target exactly the requests bound to that node.
- `remove(nodeID, conn)` — besides clearing the tracked connection (only if it is still the current one), calls `failPending(nodeID)` after releasing `cs.mu`, so a dropped control channel doesn't leave any in-flight `SendRequest` call hanging.
- `failPending(nodeID)` — walks `pending`, and for every entry whose `nodeID` matches, delivers a nil frame on its channel (non-blocking send, so a request that already got a real response is untouched). `SendRequest` treats a nil frame as "connection lost" and returns `ErrNodeDisconnected`.
- **Caveat — no idempotency:** this only fails the *waiter* fast; it does not know or control whether the node actually applied the command before the socket dropped. There is no tunneled-write idempotency key yet (would need node-side dedup support in `mymatasan`), so a non-idempotent write in flight at disconnect time has an **unknown outcome** and is **not** automatically retried. Callers/UI should treat `ErrNodeDisconnected` on a write as "unknown — verify before retrying," not as a safe-to-resend failure.

### ControlSender Interface

```go
SendRequest(ctx context.Context, nodeID string, req control.Request) (control.Response, error)
```

The node proxy API (`apps/myseliasan/apis/node_proxy.go`) depends on this narrow interface.

## Notes

- Connection tracking + request/response tunnel + node→parent event push all route through this channel.
- The server listens on a separate port (`defaultControlPort = 49533`) from the node's mTLS management port (`49532`).
- `ErrNodeOffline` and `ErrNodeDisconnected` are both surfaced to API callers as a `404 node is not connected` (see `node_proxy.go.md`) — the former means the node was never connected, the latter that it disconnected mid-command.
- `IsConnected` is wired into `INodeRegistry.SetControlPresence` in `app.go` (called after the control server is built, before the heartbeat goroutine starts). This makes a live control connection the authoritative liveness signal; the mTLS heartbeat probe becomes a fallback that can no longer flap a connected node offline.
- `IsListening`/`ConnectedCount` are advisory-only readiness signals: `app.go` stashes the `*ControlServer` on the module and surfaces them via `ReadinessStatus` on `GET /api/ready`. They never gate the process's `ok`/HTTP status — that stays db + cache only — so a dead control-channel listener alone won't flip the process to unhealthy; it's visibility for an operator/monitor, not a liveness gate.
