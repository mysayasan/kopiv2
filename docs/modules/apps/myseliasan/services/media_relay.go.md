# Module: apps/myseliasan/services/media_relay.go

## Purpose

Implements the parent side of the camera media relay. `MediaRelayHub` accepts node-dialed media connections (fleet mTLS WebSocket), and per browser subscription allocates a `streamID`, asks the node to start that camera, feeds the relayed RTP into a `stream.Subscription`, and exposes a `stream.Connector` so the `nodeMediaApi` can hand that subscription to the `stream.Manager` for WebRTC answer creation.

The browser talks only to `myseliasan`; the node→parent leg is the binary RTP channel; the parent→browser leg is WebRTC. The parent needs no GOP cache or fan-out — each subscription is an independent node stream with its own keyframe backlog.

## Type: `MediaRelayHub`

### Constructor

`NewMediaRelayHub(logf)` — builds the hub with an empty node-connection map.

### Key Methods

| Method | Description |
|---|---|
| `HandleConn(nodeID, conn)` | `mediarelay.ConnectHandler` — runs for the connection lifetime. Closes any stale connection for the same node before registering the new one. Dispatches inbound frames to active subscriptions. |
| `IsConnected(nodeID) bool` | Reports whether a node currently holds a live media connection (used by `nodeMediaApi` to return 404 rather than start a WebRTC session with no relay). |
| `Connector(nodeID) stream.Connector` | Returns a `relayConnector` bound to `nodeID`, pluggable into `stream.NewManagerWithConnectorEngine` so the WebRTC layer treats relayed RTP exactly as it treats a local camera RTP subscription. |
| `SetOwnershipHooks(onConnect, onDisconnect func(nodeID string))` | Registers callbacks fired (each in its own goroutine) when a node's media connection is accepted and when it is torn down. Set once at startup, before the media listener starts accepting. |

### Ownership hooks (Phase 4 — live camera video across instances)

`onConnect`/`onDisconnect` are what let another instance find out which instance currently holds a
node's media channel: `app.go` wires them to a `MediaOwnerRegistry.Claim`/`Release`
(`node_owner.go.md`), the same lease-in-shared-cache mechanism the control channel already uses
for command forwarding (`node_peer.go.md`). `HandleConn` calls `onConnect` right after registering
the new connection (closing any stale one first) and calls `onDisconnect` **after** the connection
has already been removed from `nodes` and every subscription closed — so a peer instance reacting
to the disconnect can never observe this instance as still holding the channel. Both hooks are
optional (`nil`-checked); a hub with none set behaves exactly as it did before Phase 4.

### Subscription Lifecycle

`subscribe(nodeID, camID)` allocates a `relaySub` (buffered packet/audio/meta/err channels), registers it under a new `streamID`, sends `FrameStart` to the node, and waits up to `relayMetaTimeout` (15 s) for the node to send `FrameMeta`. On success returns a `stream.Subscription` backed by the relay channels. On timeout or node error, sends `FrameStop` and returns an error.

`stopStream` sends `FrameStop` to the node and closes the local subscription (once, via the `closed` guard on `relaySub`).

### Frame Dispatch

The `HandleConn` read loop calls `dispatch(nc, frame)`:

- `FrameMeta` → deliver to the waiting `relaySub.meta` channel (non-blocking; the subscribe caller receives it).
- `FrameBacklog` / `FrameVideoRTP` → unmarshal RTP and deliver to `relaySub.packets` (drop on full buffer — live media, stalled browser).
- `FrameAudioRTP` → deliver to `relaySub.audio`.
- `FrameError` → deliver to `relaySub.errc` and close the subscription.
- `FrameHello` → advisory, no action (identity is proven by mTLS).

### `relayConnector`

Implements `stream.Connector`. `Subscribe(source)` parses the camera id from `source.ID` (format `"camera-N"`) and delegates to `MediaRelayHub.subscribe`. Used as the connector for a per-request `stream.Manager` in `nodeMediaApi`.

## Notes

- `relayPacketBuffer` = 1024 packets; overflow drops without blocking the node read loop (standard for live media).
- On node disconnect, `closeAll` closes every active `relaySub` so `pumpRTP` goroutines in the WebRTC layer exit cleanly (channel close is the termination signal).
- `relaySub.close()` is idempotent (guarded by `closed bool + sync.Mutex`).
- `nextStreamID` is an atomic counter, incremented for each new subscription regardless of node or camera, so IDs are globally unique on the hub.
- On a clustered deployment, `IsConnected` and `Connector` are only ever called with a `nodeID`
  this instance actually holds — a browser offer for a node held elsewhere is forwarded to the
  owning instance before it reaches this hub at all (`apis/node_media.go.md`,
  `services/media_peer.go.md`); this file itself has no cluster awareness beyond the ownership
  hooks above.
