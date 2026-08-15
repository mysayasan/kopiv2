# Module: apps/myseliasan/apis/node_media.go

## Purpose

Answers browser WebRTC offers for node cameras by re-broadcasting the RTP the node relays over its media channel. The browser peers with `myseliasan`; the node→parent transport is the binary media channel; the parent→browser transport is WebRTC.

## Endpoints

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/api/nodes/{id}/cameras/{cam}/webrtc/offer` | session | Accept a browser SDP offer for camera `{cam}` on node `{id}`, return an SDP answer. |
| `GET` | `/api/node-stream/config` | session | Return the ICE server list the browser should use for the parent↔browser WebRTC leg (empty for same-LAN; TURN when parent is behind NAT). |

## Constructor

`NewNodeMediaApi(router, auth, hub, access, engine, ice, session, forward)` — registers both route
groups and returns the built `*nodeMediaApi` (Phase 4: previously returned nothing, since nothing
needed to call back into it). The `session *AccessSessionMidware` parameter enables live-role
resolution for media access authorization. `forward func(context.Context, services.MediaOfferRequest)
(services.MediaOfferReply, error)` negotiates an offer on the instance that actually holds the
node's media channel; `nil` on a standalone install or when `cluster.advertiseUrl` is unset — see
"Cross-instance forwarding (Phase 4)" below. Must be registered before the proxy catch-all
(`/api/nodes/{id}/proxy/...`) so the specific offer path wins.

## `POST .../webrtc/offer` Flow

1. Parse `nodeID` and `camID` from the path.
2. Authorize: resolve the caller's `NodeAccessGrant` via `INodeAccessService.Resolve`; viewer access is sufficient (read-only node access). This happens **before** any forwarding decision, so authorization always runs against the operator's live session on the instance the browser actually talked to.
3. Check `MediaRelayHub.IsConnected(nodeID)`. If not connected HERE and `forward != nil`, call it with the decoded offer and return its answer verbatim on success (see below); only when forwarding is unavailable or fails does this return the pre-existing 404 `"node media channel is not connected"`.
4. Decode `{type, sdp}` SDP offer from the request body (capped 2 MiB) — happens before step 3's forwarding decision so a malformed offer is rejected the same way regardless of where the node lives.
5. Build a per-request `stream.Manager` backed by `hub.Connector(nodeID)` and the configured `WebRTCEngine` (public-IP/UDP-mux for cross-network peers, or `nil` for same-LAN).
6. Call `manager.CreateWebRTCAnswerWithOptions` — this sends `FrameStart` to the node (via `relayConnector.Subscribe`), waits for `FrameMeta`, builds H264 video and optional audio tracks, answers the SDP offer, and starts pumping relayed RTP into the browser peer.
7. Return the SDP answer.

## Cross-instance forwarding (Phase 4)

When the node's media channel terminates on another instance, this handler no longer dead-ends at
404. It calls `forward(ctx, MediaOfferRequest{NodeID, CameraID, Type, SDP})`
(`services/media_peer.go.md`'s `PeerClient.ForwardMediaOffer`, wired in `app.go`), which POSTs the
offer to the owning instance's `PeerMediaOfferPath` and returns its SDP answer. That answer is
returned to the browser exactly as if negotiated locally — it carries the OWNING instance's own
ICE candidates, so the browser's WebRTC peer connects directly to that instance and the video
itself never crosses the load balancer or this instance. If forwarding fails (owner unreachable,
stale claim), the handler falls through to the same 404 the viewer would have seen anyway — from
here the camera is unreachable either way, and that is what the viewer needs to be told.

### `AnswerLocalOffer` — the receiving side

`(*nodeMediaApi) AnswerLocalOffer(ctx, req services.MediaOfferRequest) (services.MediaOfferReply, error)`
performs the actual local negotiation (steps 5-6 above) against THIS instance's `hub`/`engine`,
using `req.NodeID`/`req.CameraID` instead of path variables. It is what
`services.NewPeerMediaOfferHandler` (`media_peer.go.md`) calls when another instance forwards an
offer here. It does **not** re-authorize: the forwarding instance already resolved the operator's
grant against their live session before forwarding, and the endpoint is reachable only with the
cluster-internal peer token.

## Configuration

- `WebRTCEngine` (from `stream.NewWebRTCEngine`) controls NAT 1:1 IP advertisement and a shared UDP mux port for all browser peers. Nil means per-peer default pion behavior (host candidates, ephemeral ports), correct for same-LAN/local dev.
- `ice []stream.ICEServer` is passed to pion as the offered ICE server list for the browser; returned verbatim by `GET /api/node-stream/config`.

## Notes

- Authorization uses the per-node access grant, not the accessrbac matrix; viewer-level grant is sufficient for live view (no write access to the node is needed). The caller's role is resolved **live from the user store** (via `AccessSessionMidware.CurrentPrincipal`) on every offer, so a just-demoted account immediately loses media access without a re-login.
- Each browser offer produces one independent `stream.Manager` (and one `relaySub` on the hub); the node sends a separate keyframe backlog for each.
- The `GET /api/node-stream/config` prefix is `/node-stream` rather than `/nodes/{id}` to avoid ambiguity with the proxy catch-all route.
- Standalone, or with `cluster.advertiseUrl` unset, `forward` is `nil` and step 3's forwarding
  branch never executes — behavior is identical to before Phase 4.
- **Not exercised end-to-end with real cameras**: `services/media_peer.go.md`'s hop is
  unit-tested at its seam, but the live two-instance bench used a `mypintusan` door controller
  (no cameras), so cross-instance video has not actually been watched in a browser.
