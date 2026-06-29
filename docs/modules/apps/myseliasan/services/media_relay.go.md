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
