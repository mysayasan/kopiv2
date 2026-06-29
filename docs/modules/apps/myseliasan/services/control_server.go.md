# Module: apps/myseliasan/services/control_server.go

## Purpose

Implements the parent side of the bi-directional parent↔node control channel. Accepts node-initiated WebSocket-over-mTLS connections, tracks live connections per node, and tunnels request/response frames.

## Key Type: ControlServer

Built via `NewControlServer(registry, port, onEvent, logf)`. Default port: `49533`.

### Responsibilities

- Accepts incoming node connections (nodes dial the parent, not the other way round) authenticated via fleet-CA mTLS.
- Maintains `conns map[nodeID → *control.Conn]` of live connections.
- `SendRequest(ctx, nodeID, req)` — tunnels a `control.Request` frame to the node and waits for the correlated `control.Response`. Returns `ErrNodeOffline` when the node has no live connection. Falls back to `controlRequestTimeout` (30s) when the caller's context has no deadline.
- `IsConnected(nodeID string) bool` — reports whether `nodeID` currently holds a live control-channel connection. Exposed to `INodeRegistry.SetControlPresence` so the heartbeat reconciler can treat a live connection as the authoritative online signal before falling back to the mTLS poll.
- Receives node-pushed event frames and dispatches them to the optional `NodeEventHandler` callback (`onEvent`).

### ControlSender Interface

```go
SendRequest(ctx context.Context, nodeID string, req control.Request) (control.Response, error)
```

The node proxy API (`apps/myseliasan/apis/node_proxy.go`) depends on this narrow interface.

## Notes

- Connection tracking + request/response tunnel + node→parent event push all route through this channel.
- The server listens on a separate port (`defaultControlPort = 49533`) from the node's mTLS management port (`49532`).
- `ErrNodeOffline` is surfaced to API callers as a `404 node is not connected`.
- `IsConnected` is wired into `INodeRegistry.SetControlPresence` in `app.go` (called after the control server is built, before the heartbeat goroutine starts). This makes a live control connection the authoritative liveness signal; the mTLS heartbeat probe becomes a fallback that can no longer flap a connected node offline.
