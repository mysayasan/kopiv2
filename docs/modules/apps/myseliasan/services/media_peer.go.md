# Module: apps/myseliasan/services/media_peer.go

## Purpose

Live camera video across instances. A node's camera RTP arrives on its MEDIA channel, which —
like the control channel (`node_peer.go.md`) — terminates on exactly one instance. Only that
instance can answer a browser's WebRTC offer for one of its cameras, so behind a load balancer a
viewer had a 1-in-N chance of landing somewhere that could serve them, and otherwise got "node
media channel is not connected" for a camera that was streaming perfectly well.

**The fix forwards the NEGOTIATION, not the media.** The offer is passed to the owning instance,
which builds the answer with its own WebRTC engine; the answer carries THAT instance's own ICE
candidates, so the browser then peers with it DIRECTLY and the video never crosses the load
balancer or a second instance. Relaying the RTP itself would have doubled bandwidth and added a
hop of latency to every frame, to move bytes that are already designed to go peer-to-peer. This is
why each instance needs its own reachable address and UDP port for live video — the operator
checklist has required this since deployment mode was introduced, because the media path was
always going to be direct.

## Key Types and Functions

- `PeerMediaOfferPath = "/internal/cluster/media-offer"` — mounted at `POST
  /api/internal/cluster/media-offer`, deliberately outside the session-auth middleware; its caller
  is a peer instance holding the derived peer token, not a signed-in user.
- `MediaOfferRequest{NodeID, CameraID, Type, SDP}` — a browser's SDP offer, forwarded verbatim to
  the instance holding the node's media channel.
- `MediaOfferReply{Type, SDP, Error}` — the owning instance's SDP answer (or an error).
- `ErrMediaNotConnected` — no instance holds this node's media channel; same message
  (`"node media channel is not connected"`) the single-instance path already returned, so callers
  don't have to distinguish "not connected here" from "not connected anywhere."
- `(*PeerClient).ForwardMediaOffer(ctx, ownerURL, req) (MediaOfferReply, error)` — reuses
  `node_peer.go.md`'s `PeerClient` (same derived token, same HTTP client) to POST the offer to the
  owning instance and return its answer. A non-200 response or a non-empty `reply.Error` becomes a
  Go `error`.
- `PeerMediaOfferHandler` — the receiving half. `NewPeerMediaOfferHandler(jwtSecret, answer, logf)`
  derives the peer token via `DerivePeerToken` (`node_peer.go.md`) and stores `answer`, a callback
  the API layer supplies that performs the actual local WebRTC negotiation (the API layer owns the
  engine, not this package). `ServeHTTP` checks the bearer token with the shared
  `peerTokenMatches` (`node_peer.go.md`), decodes and validates the offer (non-empty `NodeID`,
  non-zero `CameraID`), then calls `answer`. A negotiation failure is returned as `200` with
  `{"error": "..."}` — the HOP succeeded, the negotiation did not — matching `PeerForwardHandler`'s
  convention in `node_peer.go.md`.

## Authorization is deliberately NOT repeated on the receiving side

The forwarding instance already resolved the operator's live session and their per-node access
grant before forwarding (`apis/node_media.go.md`'s `offer` handler). This endpoint is reachable
only with the cluster-internal peer token; re-resolving the grant here would mean trusting a role
id carried on the wire instead of the session that produced it, which is a weaker check, not a
redundant one.

## Notes

- Wired in `apps/myseliasan/app/app.go` (`app.go.md`'s "Cross-instance media forwarding (Phase
  4)" section): a `MediaOwnerRegistry` (`node_owner.go.md`) tracks which instance holds each
  node's media channel; when an offer's node is owned elsewhere, `apis.nodeMediaApi.offer`
  (`apis/node_media.go.md`) calls the `forward` func built here instead of negotiating locally;
  `PeerMediaOfferHandler` on the owning instance calls back into
  `apis.nodeMediaApi.AnswerLocalOffer`, which performs the negotiation exactly as the
  single-instance path always has.
- The command hop (`node_peer.go.md`) and this media hop share one `PeerClient` per instance
  (`clusterPeer` in `app.go`) and the same derived token — there is no separate media-forwarding
  secret to configure.
- Standalone (`cluster.advertiseUrl` empty, the default), `MediaOwnerRegistry.Enabled()` is
  `false`, `forward` is never built (`nil`), and `PeerMediaOfferHandler` is never mounted — every
  offer negotiates locally exactly as before this file existed.
- Covered by `media_peer_test.go` (8 cases): forwarding + answer round-trip (the answer's SDP and
  the request's node/camera survive the hop intact), 401 on a missing or wrong bearer token, the
  owner's "not connected" error propagates back to the caller, malformed/incomplete offers are
  rejected with 400 before negotiation is attempted, an unreachable owner surfaces as an error
  rather than hanging, and — sharing `node_owner_test.go`'s fake cache store —
  `TestMediaOwnershipIsTrackedSeparatelyFromControl` and
  `TestMediaReleaseLeavesControlOwnershipIntact` prove the control-channel and media-channel
  claims for the same node are resolved and released independently. **Not exercised
  end-to-end with real cameras**: the live two-instance bench used a `mypintusan` door
  controller, which has no cameras, so cross-instance video has not been watched in a browser.
