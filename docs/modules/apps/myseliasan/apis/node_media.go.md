# Module: apps/myseliasan/apis/node_media.go

## Purpose

Answers browser WebRTC offers for node cameras by re-broadcasting the RTP the node relays over its media channel. The browser peers with `myseliasan`; the node→parent transport is the binary media channel; the parent→browser transport is WebRTC.

## Endpoints

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/api/nodes/{id}/cameras/{cam}/webrtc/offer` | session | Accept a browser SDP offer for camera `{cam}` on node `{id}`, return an SDP answer. |
| `GET` | `/api/node-stream/config` | session | Return the ICE server list the browser should use for the parent↔browser WebRTC leg (empty for same-LAN; TURN when parent is behind NAT). |

## Constructor

`NewNodeMediaApi(router, auth, hub, access, engine, ice, session)` — registers both route groups. The `session *AccessSessionMidware` parameter enables live-role resolution for media access authorization. Must be registered before the proxy catch-all (`/api/nodes/{id}/proxy/...`) so the specific offer path wins.

## `POST .../webrtc/offer` Flow

1. Parse `nodeID` and `camID` from the path.
2. Authorize: resolve the caller's `NodeAccessGrant` via `INodeAccessService.Resolve`; viewer access is sufficient (read-only node access).
3. Check `MediaRelayHub.IsConnected(nodeID)`; return 404 when the node's media channel is not up.
4. Decode `{type, sdp}` SDP offer from the request body (capped 2 MiB).
5. Build a per-request `stream.Manager` backed by `hub.Connector(nodeID)` and the configured `WebRTCEngine` (public-IP/UDP-mux for cross-network peers, or `nil` for same-LAN).
6. Call `manager.CreateWebRTCAnswerWithOptions` — this sends `FrameStart` to the node (via `relayConnector.Subscribe`), waits for `FrameMeta`, builds H264 video and optional audio tracks, answers the SDP offer, and starts pumping relayed RTP into the browser peer.
7. Return the SDP answer.

## Configuration

- `WebRTCEngine` (from `stream.NewWebRTCEngine`) controls NAT 1:1 IP advertisement and a shared UDP mux port for all browser peers. Nil means per-peer default pion behavior (host candidates, ephemeral ports), correct for same-LAN/local dev.
- `ice []stream.ICEServer` is passed to pion as the offered ICE server list for the browser; returned verbatim by `GET /api/node-stream/config`.

## Notes

- Authorization uses the per-node access grant, not the accessrbac matrix; viewer-level grant is sufficient for live view (no write access to the node is needed). The caller's role is resolved **live from the user store** (via `AccessSessionMidware.CurrentPrincipal`) on every offer, so a just-demoted account immediately loses media access without a re-login.
- Each browser offer produces one independent `stream.Manager` (and one `relaySub` on the hub); the node sends a separate keyframe backlog for each.
- The `GET /api/node-stream/config` prefix is `/node-stream` rather than `/nodes/{id}` to avoid ambiguity with the proxy catch-all route.
