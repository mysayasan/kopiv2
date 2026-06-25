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
| `ParentBaseURL` | Externally reachable URL recorded on the node so it can call back (enroll / release / self-drop). |
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
- `Heartbeat(ctx)` — probe every adopted node (excluding `"self-dropped"`) over mTLS (`GET /heartbeat`); mark `online` + bump `LastSeenAt` on success, `lost` on failure. Called by the background loop started in `app.go`.
- `MarkSelfDropped(ctx, nodeID, nonce, ts, assertion)` — verify the fleet-key assertion (5-minute window) before updating the node's `Status` to `"self-dropped"`.

## HTTP Clients

Two clients are in use:

- **Bootstrap client** (`InsecureSkipVerify: true`) — used for the PSK adoption call and the token-based release fallback; the node is identified by IP+port before any cert exists.
- **mTLS client** — built per-node on demand via `mtlsClient(ctx, node)`, which calls `fleetCA.ParentClientTLS` to obtain the parent cert and `fleetca.ClientTLSConfig` with `expectServerCN = nodeID`. Used for heartbeat probes and preferred-path release.

## Parent Identity

`NewNodeRegistry` receives `ParentID`, `ParentName`, and `ParentBaseURL`. These are stamped into every adopt call so the node knows its parent. `ParentBaseURL` is the control-plane's externally reachable URL (from `sso.redirectBaseUrl`); the node uses it for enroll and self-drop callbacks.

## Errors

| Error | Meaning |
|---|---|
| `ErrFleetKeyUnset` | Scan or adopt attempted without a fleet key. |
| `ErrNodeAlreadyKnown` | Node ID already in the registry (reserved; upsert is idempotent). |
| `ErrAdoptRejected` | Node returned a non-2xx response, or token mismatch on enroll. |
| `ErrNodeRevoked` | Enroll attempted for a node whose cert has been revoked. |
| `ErrNodeUnknown` | Enroll attempted for a node ID not in the registry. |
