# Module: apps/myseliasan/services/node_registry.go

## Purpose

Implements `INodeRegistry`, the control-plane service for fleet-key management, LAN discovery, and the full adopt/release/self-drop/enroll/heartbeat lifecycle of `mymatasan` nodes. Also owns the on-prem fleet CA (`fleetCA`) used for post-adoption mTLS.

## Constructor

`NewNodeRegistry(db, NodeRegistryConfig)` — builds the registry. `NodeRegistryConfig` fields:

| Field | Meaning |
|---|---|
| `MulticastAddr` | UDP multicast group+port for discovery (empty → package default). |
| `ParentID` | This control plane's identity (stamped into adopt calls and used as the CA CN for the parent cert). |
| `ParentName` | Human-readable control-plane name. |
| `ParentBaseURL` | URL recorded on the node for callbacks (enroll / release / self-drop) and as the host it dials for the control and media channels. Derived in `app.go` from `pairing.parentBaseUrl` (when set) or `sso.redirectBaseUrl`; must be a LAN-reachable address (not localhost) when node and parent run on separate machines. |
| `MTLSPort` | Node mTLS management listener port recorded at adoption (default 49532). |
| `CertTTL` | Lifetime of issued node certificates (default 7 days). |
| `HeartbeatInterval` | Interval for the background heartbeat loop (managed by app.go, not the registry itself). |

## Responsibilities

- `FleetKey` / `SetFleetKey` / `GenerateFleetKey` — read, set (minimum 16 chars), or generate (32-byte CSPRNG, base64 raw-URL) the shared HMAC fleet key, persisted in `ControlSetting` under `pairing.fleetKey`.
- `List` — return all adopted `ManagedNode` rows (up to 1000).
- `Scan(ctx, timeout)` — call `pairing.Discover` with the current fleet key and multicast address, then annotate each discovered node with `adopted: true/false` by cross-referencing the local registry. Returns `ErrFleetKeyUnset` when no key is configured.
- `Adopt(ctx, AdoptInput)` — build a fleet-key-signed adopt request (`pairing.SignAssertion` over parentId + nonce + ts), POST it to `https://<ip>:<port>/api/pairing/adopt`, un-revoke the node's cert (to allow re-adoption), store the returned token + `MTLSPort` in a `ManagedNode` row (upsert by NodeId), and return the saved row.
- `Enroll(ctx, nodeID, token, csrPEM)` — token-authenticated CSR signing. Looks up the adopted node by `nodeID`, verifies the pairing token matches, calls `fleetCA.SignNodeCSR`, records `CertExpiresAt` + bumps `LastSeenAt`, and returns the node cert PEM + CA root PEM. Used for both initial enrollment and renewal.
- `Release(ctx, nodeID)` — revoke the node's cert (so it cannot renew), attempt to notify the node to release via mTLS (`POST /release`) falling back to the token leg, then unconditionally delete the `ManagedNode` row.
- `SetControlPresence(connected func(nodeID string) bool)` — injects the control-channel liveness oracle from `ControlServer.IsConnected`. Called once at startup (after the control server is built in `app.go`). When set, a node holding a live control connection is treated as authoritatively online by `Heartbeat` regardless of the mTLS poll result.
- `Heartbeat(ctx)` — reconciles every adopted node's liveness (excluding `"self-dropped"`). The control channel is consulted first via the injected presence function; the mTLS poll is only a fallback. A node is marked `"lost"` only after a grace window with no contact on either path (`lostGraceSeconds` = 3× heartbeat interval, floor 90 s). Within the grace window, the prior status is held and no database write is performed, preventing brief reconnect events from flapping a healthy node offline. Called by the background loop started in `app.go`.
- `MarkSelfDropped(ctx, nodeID, nonce, ts, assertion)` — verify the fleet-key assertion (5-minute window) before updating the node's `Status` to `"self-dropped"`.

## HTTP Clients

Two clients are in use:

- **Bootstrap client** (`InsecureSkipVerify: true`) — used for the PSK adoption call and the token-based release fallback; the node is identified by IP+port before any cert exists.
- **mTLS client** — built per-node on demand via `mtlsClient(ctx, node)`, which calls `fleetCA.ParentClientTLS` to obtain the parent cert and `fleetca.ClientTLSConfig` with `expectServerCN = nodeID`. Used for heartbeat probes and preferred-path release.

## Parent Identity

`NewNodeRegistry` receives `ParentID`, `ParentName`, and `ParentBaseURL`. These are stamped into every adopt call so the node knows its parent. `ParentBaseURL` is the address the node uses for enroll and self-drop callbacks and as the host it dials for the persistent control and media channels. `app.go` derives it from `pairing.parentBaseUrl` (when set) or falls back to `sso.redirectBaseUrl`. On a deployment where node and control plane are on separate machines, `pairing.parentBaseUrl` must be set to the parent's LAN-reachable URL (never `localhost`).

## Errors

| Error | Meaning |
|---|---|
| `ErrFleetKeyUnset` | Scan or adopt attempted without a fleet key. |
| `ErrNodeAlreadyKnown` | Node ID already in the registry (reserved; upsert is idempotent). |
| `ErrAdoptRejected` | Node returned a non-2xx response, or token mismatch on enroll. |
| `ErrNodeRevoked` | Enroll attempted for a node whose cert has been revoked. |
| `ErrNodeUnknown` | Enroll attempted for a node ID not in the registry. |
