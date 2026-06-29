# Module: infra/mediarelay/server.go

## Purpose

Parent-side TCP listener for the media channel. Accepts node-initiated WebSocket-over-fleet-mTLS connections and dispatches them to a `ConnectHandler` (the `MediaRelayHub`).

## Key Types

### `Server`

Built via `NewServer(addr, tlsCfg, onConn, logf)`. `addr` is `":port"` (e.g. `":49534"`). `tlsCfg` must require and verify the node client certificate against the fleet CA (produced by `INodeRegistry.ParentServerTLS`).

- `Run(ctx)` — starts an HTTPS server (TLS from `tlsCfg`), handles `GET /media` as a WebSocket upgrade. Shuts down cleanly when `ctx` is cancelled. Returns nil on clean shutdown, the listener error otherwise.
- `handle` — extracts `nodeID` from the verified TLS client cert CN (via `fleetca.PeerCommonName`); rejects connections with no cert (HTTP 401); upgrades to WebSocket; calls `onConn(nodeID, conn)`.

### `ConnectHandler`

```go
type ConnectHandler func(nodeID string, c *Conn)
```

Must block for the connection lifetime (the read loop). Returning from the handler closes the connection. Implemented by `MediaRelayHub.HandleConn`.

## Notes

- Origin check is disabled (`CheckOrigin` returns true) because authentication is the fleet-CA mTLS layer, not the HTTP Origin header.
- The WebSocket upgrader uses 64 KiB read/write buffers and a 15 s handshake timeout.
- Run on its own goroutine in `myseliasan/app/app.go`; TLS config is obtained asynchronously (after the fleet CA has initialized) so a startup race between the CA and the listener is handled by the goroutine gate.
