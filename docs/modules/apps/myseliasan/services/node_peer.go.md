# Module: apps/myseliasan/services/node_peer.go

## Purpose

The instance-to-instance hop. `node_owner.go.md`'s `NodeOwnerRegistry` answers *which* instance
holds a node's control channel; this file delivers the request to it when that instance isn't the
one that received the browser call. That makes it a new inbound network surface carrying
operator-authorized node commands (unlocking doors, changing settings), so it is authenticated as
carefully as the operator-facing API.

## Why the credential is derived, not configured

The peer token is **derived** from `jwt.secret` (via HKDF-SHA256) rather than being a new secret an
operator must set identically on every instance. A separate cluster secret would isolate blast
radius, but it would be one more value that has to be identical across the deployment — exactly the
class of mistake this whole feature exists to catch. The derivation is one-way, so the token never
reveals the signing secret; and if the signing secret had leaked, an attacker could already mint
superadmin tokens for the public API, which is strictly more power than this endpoint grants — so
deriving from it adds no new weakness.

`peerTokenInfo` (`"kopiv2-cluster-peer-token-v1"`) domain-separates the peer token from every other
value derived from the same secret — notably the boot-log fingerprint (`infra/atrest.FingerprintSecret`,
`infra/atrest/cipher.go.md`) — so possessing one can never produce the other, and reading a log file
that prints the fingerprint cannot be used to call this endpoint. `TestPeerTokenIsNotTheBootFingerprint`
(`node_peer_test.go`) asserts the two derivations differ and that neither is a prefix of the other.

## Key functions and types

- `DerivePeerToken(jwtSecret string) string` — HKDF-SHA256 over `jwtSecret`, `peerTokenInfo`, 32
  bytes, hex-encoded. Every instance derives the same value from the same shared signing secret
  without transmitting it. Empty `jwtSecret` returns `""`.
- `PeerForwardPath = "/internal/cluster/node-forward"` — mounted at `POST /api/internal/cluster/node-forward`.
  Under `/api` so it shares the TLS listener and the reset gate, but deliberately mounted OUTSIDE
  the session-auth middleware: its caller is a peer instance holding a derived token, not a
  signed-in user.
- `PeerClient` — built via `NewPeerClient(jwtSecret string, timeout time.Duration, insecureSkipVerify bool) *PeerClient`.
  `Forward(ctx, ownerURL, nodeID, req control.Request) (control.Response, error)` POSTs the request
  to the owning instance's `PeerForwardPath` with `Authorization: Bearer <token>`, decodes the JSON
  reply, and surfaces a node-level failure the owner reported (offline, disconnected mid-flight) as
  a Go `error` so the caller's existing handling applies unchanged. `insecureSkipVerify` exists
  because instances default to self-signed certificates; the request is authenticated by the
  derived token either way, so this trades transport privacy between instances, not admission.
- `ForwardingSender` — decorates `ControlSender` (`services/control_server.go.md`'s narrow
  interface both the node command proxy and the recording-stream reader already depend on).
  `NewForwardingSender(local ControlSender, owners *NodeOwnerRegistry, peer *PeerClient, logf) *ForwardingSender`.
  `SendRequest(ctx, nodeID, req)` resolves the owner via `owners.OwnerOf`: local or unowned goes
  straight to `local.SendRequest` (unowned so the caller still sees `ErrNodeOffline`, unchanged);
  owned elsewhere is forwarded via `peer.Forward`, and a forwarding failure is wrapped in
  `ErrNodeOffline` — a stale claim (the owning instance died between renewals) is both true from
  here and the error the caller already knows how to render. One decorator teaches both surfaces
  cluster awareness without either learning that instances exist; standalone it is a pass-through.
- `peerTokenMatches(r *http.Request, token string) bool` — the shared bearer-token check
  (extracted in Phase 4 so `node_peer.go`'s command hop and `media_peer.go`'s camera-negotiation
  hop can never drift apart into two different comparisons). Constant-time
  (`crypto/subtle.ConstantTimeCompare`): an early-exit compare would leak the token one byte at a
  time to anyone who can measure the reply. Every peer endpoint in the codebase — this file's
  `PeerForwardHandler` and `media_peer.go.md`'s `PeerMediaOfferHandler` — calls this one function
  rather than each rolling its own comparison.
- `PeerForwardHandler` — the receiving half. `NewPeerForwardHandler(jwtSecret string, local ControlSender, logf) *PeerForwardHandler`.
  `ServeHTTP` checks the bearer token with `peerTokenMatches` above, decodes the forwarded body,
  and calls `local.SendRequest` directly. **Must be constructed with the LOCAL sender (the
  `ControlServer`), never a
  `ForwardingSender`** — this is what makes the hop terminal: an instance named as the owner either
  has the connection or the claim is stale, and forwarding onward from here would let a stale claim
  bounce a request between instances instead of failing it cleanly. A node-level failure is
  returned as `200` with `{"error": "..."}` — the HOP succeeded, the node call did not — so the node
  being offline never looks like the peer being unreachable.
- `ErrNoOwner` — no instance currently holds the node's control channel (offline everywhere, not
  merely elsewhere). Declared but not yet returned by any exported path in this file — `OwnerOf`
  returning `("", false)` is handled directly by `ForwardingSender.SendRequest`'s local fallback.

## Wire shape

`peerForwardBody`/`peerForwardReply` carry `control.Request`/`control.Response` field-by-field
rather than embedding the internal frame type, so a change to `control.Request`'s shape cannot
silently alter the contract between two instances that may be on different builds mid-rollout
(a rolling deploy).

## Notes

- Wired in `apps/myseliasan/app/app.go` (`app.go.md`): the peer client, `ForwardingSender`, and
  `PeerForwardHandler`/route are only constructed when `nodeOwners.Enabled()` — i.e. when
  `cluster.advertiseUrl` is set. Standalone, `apis.NewRecordingStreamApi`/`NewNodeProxyApi` are
  handed the bare `controlServer` and the peer endpoint is never mounted.
- `NewRecordingStreamApi` (recording playback over the tunnel) and `NewNodeProxyApi` (the reverse
  command tunnel, `/api/nodes/{id}/proxy/...`) are the two surfaces this makes cluster-aware. Live
  camera video (`NewNodeMediaApi`) is covered by a separate, parallel hop
  (`services/media_peer.go.md`) rather than by this file's `ForwardingSender`/`PeerClient`: the
  media channel is its own connection, tracked by its own `MediaOwnerRegistry`
  (`node_owner.go.md`), and only the WebRTC NEGOTIATION is forwarded — the video itself still goes
  browser-to-owning-instance directly, never through this hop or the load balancer.
- Covered by `node_peer_test.go`: token derivation is deterministic and non-empty only for a
  non-empty secret, the derived peer token is never equal to nor a prefix/suffix of the boot-log
  fingerprint derived from the same secret, and handler-level auth (missing/wrong/correct bearer
  token).
