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
| `CertWarnBefore` | How far ahead of a node certificate's expiry `Heartbeat` raises a fleet-health warning via the event sink — a still-valid cert this close to expiry means automatic re-enrollment is overdue or failing. `0` defaults to 7 days. `app.go` wires this from `pairing.renewBeforeHours` (previously read only by the node side). |

## Responsibilities

- `FleetKey` / `SetFleetKey` / `GenerateFleetKey` — read, set (minimum 16 chars), or generate (32-byte CSPRNG, base64 raw-URL) the shared HMAC fleet key, persisted in `ControlSetting` under `pairing.fleetKey`.
- `List` — return all adopted `ManagedNode` rows (up to 1000).
- `FleetStatus(ctx)` — rolls up liveness (`Total`/`Online`/`Lost`/`SelfDropped`/`Unknown`, by `ManagedNode.Status`) and certificate health (`CertsExpiring` — valid but within the warn window, `CertsExpired`, and `CertWarnDays`, the warn window in whole days) across all adopted nodes. Backs `GET /api/nodes/fleet-status` for the dashboard's "certs expiring" KPI.
- `UpdateMeta(ctx, nodeID, name, description, icon, updatedBy)` — edit an adopted node's operator-facing fields (display name, description, nav icon) after adoption; looks up the node by `NodeId`, trims and applies the fields (an empty icon leaves the existing one), stamps `UpdatedBy`/`UpdatedAt`, and persists. Never touches identity/trust fields. Returns `ErrNodeUnknown` for an unrecognized node ID.
- `Scan(ctx, timeout)` — call `pairing.Discover` with the current fleet key and multicast address, then annotate each discovered node with `adopted: true/false` by cross-referencing the local registry. Returns `ErrFleetKeyUnset` when no key is configured.
- `Adopt(ctx, AdoptInput)` — build a fleet-key-signed adopt request (`pairing.SignAssertion` over parentId + nonce + ts), POST it to `https://<ip>:<port>/api/pairing/adopt`, un-revoke the node's cert (to allow re-adoption), store the returned token + `MTLSPort` in a `ManagedNode` row (upsert by NodeId), and return the saved row. `AdoptInput` now includes `Name` (operator-chosen label, wins over the node's reported hostname), `Description` (optional tooltip shown in the nav), and `Icon` (pre-installed glyph name shown in the side-nav node tree).
- `Enroll(ctx, nodeID, token, csrPEM)` — token-authenticated CSR signing. Looks up the adopted node by `nodeID`, verifies the pairing token matches, calls `fleetCA.SignNodeCSR`, records `CertExpiresAt` + bumps `LastSeenAt`, and returns the node cert PEM + CA root PEM. Used for both initial enrollment and renewal.
- `Release(ctx, nodeID)` — revoke the node's cert (so it cannot renew), attempt to notify the node to release via mTLS (`POST /release`) falling back to the token leg, then unconditionally delete the `ManagedNode` row.
- `SetControlPresence(connected func(nodeID string) bool)` — injects the control-channel liveness oracle from `ControlServer.IsConnected`. Called once at startup (after the control server is built in `app.go`). When set, a node holding a live control connection is treated as authoritatively online by `Heartbeat` regardless of the mTLS poll result.
- `Heartbeat(ctx)` — reconciles every adopted node's liveness (excluding `"self-dropped"`). The control channel is consulted first via the injected presence function; the mTLS poll is only a fallback. A node is marked `"lost"` only after a grace window with no contact on either path (`lostGraceSeconds` = 3× heartbeat interval, floor 90 s). Within the grace window, the prior status is held and no database write is performed, preventing brief reconnect events from flapping a healthy node offline. Every sweep also runs `checkCertExpiry` for every node (independent of liveness), and on each node's status transition emits a fleet event to the injected sink (see "Fleet-health alerting" below). Called by the background loop started in `app.go`.
- `MarkSelfDropped(ctx, nodeID, nonce, ts, assertion)` — verify the fleet-key assertion (5-minute window) before updating the node's `Status` to `"self-dropped"`.

## Fleet-health alerting

The registry no longer just tracks liveness passively — it edge-triggers notifications so a crashed or partitioned node, or a cert stuck failing renewal, doesn't fail silently:

- `SetFleetEventSink(sink FleetEventSink)` — injects the callback (`func(FleetEvent)`) the registry invokes synchronously from `Heartbeat` when it detects a transition. Optional (nil-safe); `app.go` wires it once at startup, before the heartbeat loop starts, to `publishFleetEvent` (turns each event into a `notification.Notification` in the unified feed, source `"node:<id>"`).
- `FleetEventKind` values: `FleetEventNodeLost` (fires once, the instant a node crosses into `"lost"`), `FleetEventNodeRecovered` (fires once, when a previously-`"lost"` node becomes reachable again), `FleetEventCertExpiring` (fires once per distinct `CertExpiresAt` value when a node's certificate is within `CertWarnBefore` of expiry, or already expired).
- Both liveness events are strictly edge-triggered: a node that stays lost across sweeps is not re-notified (the write and the event are both skipped once already `"lost"`), and a node that stays online never fires `FleetEventNodeRecovered` (only the lost→online transition does).
- `checkCertExpiry(node, now)` dedups per-node via `certWarned` (`nodeID → last-warned CertExpiresAt`): a renewal that pushes the expiry out re-arms the warning for the next approach; a node whose cert is healthy and outside the window has its dedup entry cleared so a later approach can warn again. It never writes the node record — the expiry was persisted at enrollment.
- `FleetEvent` carries `Kind`, `Node` (the affected `*entities.ManagedNode`), and — for cert events only — `ExpiresAt` (unix) and `HoursLeft` (whole hours until expiry, negative if already expired).

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
