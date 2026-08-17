# Module: apps/myseliasan/services/node_owner.go

## Purpose

`NodeOwnerRegistry` answers one question a load-balanced deployment cannot answer locally: which
instance is holding a given node's control channel right now? A node dials IN and keeps exactly
one connection open to whichever instance it reached, so each instance's own connection map only
ever describes its own nodes. Left unaddressed that breaks two things silently:

- **Liveness.** The heartbeat reconciler treated "not in my connection map" as "not
  control-connected" and fell back to an mTLS probe; where that probe could not reach the node
  (NAT, firewall) a perfectly healthy node attached to another instance was marked `lost` and an
  operator was alerted about it.
- **Commands.** Opening a node's own screens (Settings dialog, recording playback) only worked
  when the browser request happened to land on the instance holding that node — a 1-in-N coin
  flip behind a load balancer.

The registry makes ownership a deployment-wide fact instead of a per-process one: the instance
holding a connection publishes a claim, and every instance can read it.

## A node also holds a MEDIA channel — tracked separately (Phase 4)

A node opens a SECOND, independent connection to the control plane for camera RTP (the media
channel, `media_relay.go.md`), and nothing guarantees it lands on the same instance as the control
channel. Tracking both under one key would let a command be forwarded to an instance that holds
only the media connection, or a camera offer forwarded to one that holds only the control
channel — and both would fail in a way that looks exactly like the node being down. So this file
now builds two independently-namespaced registries from one implementation:

- `ownerKeyPrefix = "nodeowner:"` — control-channel claims (unchanged from Phase 2).
- `mediaOwnerKeyPrefix = "mediaowner:"` — media-channel claims (new).

`NewNodeOwnerRegistry(store, self, ttl, logf)` and `NewMediaOwnerRegistry(store, self, ttl, logf)`
have the identical signature and both return `*NodeOwnerRegistry`; they differ only in which
prefix the shared `newOwnerRegistry(store, prefix, self, ttl, logf)` constructor is given. Every
method (`Claim`, `Release`, `OwnerOf`, `ConnectedAnywhere`, `StartRenewal`) is prefix-agnostic —
a `NodeOwnerRegistry` instance only ever reads and writes its own namespace, so a control-channel
registry and a media-channel registry sharing one cache never see each other's claims.

## Why the shared cache, not a database table

A claim is a **lease**, and a TTL that expires on its own is exactly the right behavior when an
instance dies without cleaning up. A database row would need a reaper, and a reaper that fails
leaves a node permanently "owned" by a machine that no longer exists. With the per-process
(`memory`) cache — the single-instance default — the registry degenerates to an in-process map,
which is precisely correct for one instance: it owns every connection it knows about, and there
is nobody to forward to.

## Key Type: NodeOwnerRegistry

Built via `NewNodeOwnerRegistry(store cache.Store, self string, ttl time.Duration, logf func(string, ...any)) *NodeOwnerRegistry`
(control channel, prefix `nodeowner:`) or `NewMediaOwnerRegistry(...)` (media channel, prefix
`mediaowner:`, same signature — see "A node also holds a MEDIA channel" above).
`self` is this instance's advertise URL (`cluster.advertiseUrl`, `infra/config/config_models.go.md`'s
`ClusterConfigModel`); a `nil` store or an empty `self` makes every method behave as "only local
connections exist" — the correct standalone truth. `ttl` defaults to 30s (`defaultOwnershipTTL`)
when `<= 0`.

- `Enabled() bool` — whether ownership is actually published anywhere (`store != nil && self != ""`).
- `Claim(ctx, nodeID)` — records that this instance now holds `nodeID`'s control channel, both in
  the local `held` set (used as the fast path and by the renewal loop) and, if `Enabled()`, in the
  shared cache under `nodeowner:<nodeId>`. **Last-writer-wins, deliberately not a compare-and-set**:
  a node holds exactly one connection, so if it has just reconnected to a different instance, that
  instance IS the new owner and should overwrite — refusing the write would leave the claim
  pointing at an instance that no longer has the connection, which is the exact failure this exists
  to prevent. A cache write failure is logged, not fatal: the node is connected here regardless, so
  local requests still work; only the other instances' view is stale until the next renewal tick.
- `Release(ctx, nodeID)` — withdraws this instance's claim when a control connection drops, so
  another instance can take the node over without waiting out the TTL. Deletes the cache key
  **only if the claim still points here** — a node that already reconnected elsewhere has
  overwritten the key, and deleting then would erase the new owner's claim and make a perfectly
  reachable node look unowned.
- `OwnerOf(ctx, nodeID) (owner string, local bool)` — the advertise URL of the instance holding
  `nodeID`, and whether that's this one. Checks the local `held` set first (no cache round trip for
  a node connected here); falls back to a cache lookup. An empty `owner` with `local == false`
  means nobody holds it.
- `ConnectedAnywhere(ctx, nodeID) bool` — the liveness oracle the heartbeat reconciler needs:
  whether ANY instance holds this node's control channel. `local` is checked separately from a
  non-empty owner URL because a standalone instance has no advertise URL and holds connections
  under an empty `self` — reading only the URL would report every node on every single-instance
  install as not-connected, which is exactly the failure this oracle exists to prevent.
- `StartRenewal(ctx)` — a goroutine that re-publishes every claim this instance holds on an
  interval of `ttl/3`, until `ctx` is cancelled. A no-op when `!Enabled()`. The `/3` interval is
  what makes a short TTL safe: a single missed tick (a slow cache round trip, a GC pause) cannot
  expire a claim that is genuinely still held.

## Notes

- Wired in `apps/myseliasan/app/app.go` (`app.go.md`): `controlServer.SetOnConnect` claims,
  `controlServer.SetOnDisconnect` releases (`services/control_server.go.md`), `StartRenewal` runs
  on `bgCtx`, and `registry.SetControlPresence` is switched from `controlServer.IsConnected` to
  `nodeOwners.ConnectedAnywhere` — the false-lost fix. The media-channel registry is wired the
  same way off `MediaRelayHub.SetOwnershipHooks` (`media_relay.go.md`) instead of the control
  server's connect/disconnect hooks — see `app.go.md`'s "Cross-instance media forwarding (Phase
  4)".
- The registry only answers "who owns this node's control/media channel," never delivers a
  request to it — that hop is `node_peer.go.md`'s `PeerClient`/`ForwardingSender` for commands and
  `media_peer.go.md`'s `PeerClient.ForwardMediaOffer` for camera negotiation.
- Standalone (per-process `memory` cache, the default), `Enabled()` is always `false`: every
  `Claim`/`Release`/`OwnerOf`/`ConnectedAnywhere` call is local-only, and `StartRenewal` does
  nothing. Behavior for a single instance is unchanged by this feature.
- Covered by `node_owner_test.go`: local-only standalone behavior, cross-instance resolution via a
  fake TTL-aware `cache.Store`, reconnect-transfers-ownership, release-does-not-steal-from-a-new-
  owner, release-frees-the-node-immediately, claim-expires-without-renewal, renewal-keeps-a-claim-
  alive-past-its-original-TTL, and survives-cache-write-errors. Two additional cases in
  `media_peer_test.go` (`TestMediaOwnershipIsTrackedSeparatelyFromControl`,
  `TestMediaReleaseLeavesControlOwnershipIntact`) prove a control-channel registry and a
  media-channel registry sharing one cache store resolve and release independently for the same
  node ID.
