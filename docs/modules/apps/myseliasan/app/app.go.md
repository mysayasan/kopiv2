# Module: apps/myseliasan/app/app.go

## Purpose

Implements the `myseliasan` relying control-plane app for the shared runtime host.

## Responsibilities

- Provides app identity and base directory.
- Registers `ManagedNode`, `ControlSetting`, `NodeAccessGrant`, `ControlUser`, and the shared `AccessRole`/`AccessRolePermission` entities for DB bootstrap.
- Enables only `Version` and `AccessRbac` from the shared API surface (`SharedAPIs()`); operational APIs (log, file storage, cache, app registry, etc.) are disabled for the control plane.
- Seeds the local endpoint catalog for rate limiting and runtime metadata, including `Notifications` and `Node Access` endpoints.
- On startup, seeds the stock superadmin local account via `EnsureStockSuperadmin` using credentials from `localAuth.username` / `localAuth.password` in config (or defaults `admin` / `admin`).
- Binds the `ControlUser` service as the `AccessUserResolver` for `deps.Access`, so the shared accessrbac middleware enforces the permission matrix on myseliasan's own endpoints.
- Registers auth/session routes (`NewAuthApi`, `NewSessionApi`).
- Registers myseliasan-specific RBAC admin surface (`NewRbacAdminApi` at `/api/rbac/*`) for user management and the bootstrap superadmin handoff.
- Builds `INodeRegistry` with `ParentBaseURL` derived from `pairing.parentBaseUrl` (when set) or `sso.redirectBaseUrl` (fallback). `pairing.parentBaseUrl` must be the parent's LAN-reachable URL for deployments where node and parent are on separate machines.
- Registers node-management routes (`NewNodesApi`).
- Starts the control channel server (`ControlServer`) on a dedicated fleet-mTLS port (`pairing.controlPort`, default `49533`). After the server is built, wires `ControlServer.IsConnected` into the registry via `SetControlPresence` so the heartbeat treats a live control connection as the authoritative online signal (mTLS poll becomes a fallback).
- Starts a background heartbeat goroutine (after `SetControlPresence` is wired) to reconcile node liveness with grace-window flap protection.
- Wires proactive fleet-health alerting: `registry.SetFleetEventSink` is set (before the heartbeat loop starts) to a closure that calls `publishFleetEvent`, so a node going online→lost, a lost node recovering, or a certificate nearing expiry (per `CertWarnBefore`, derived from `pairing.renewBeforeHours`) is surfaced in the unified notification feed instead of failing silently.
- Builds the unified notification service and registers `NewNotificationApi`. Node-pushed events are ingested into the notification feed via `ingestNodeEvent`.
- Registers per-node access-grant management (`NewNodeAccessApi`). The node access service is constructed with the roles service (`NewNodeAccessService(db, roleService)`) so superadmin roles receive implicit full node access. All three node APIs (`NewNodeAccessApi`, `NewNodeMediaApi`, `NewNodeProxyApi`) now accept the `controlSession *AccessSessionMidware` so they resolve the caller's live role on every request.
- Starts the node camera media relay: builds a `stream.WebRTCEngine` (from `nodeStream.publicIps` / `nodeStream.udpPort`; nil for same-LAN), starts a `mediarelay.Server` on `pairing.mediaPort` (default `49534`) using the fleet-CA mTLS server config, registers `NewNodeMediaApi` (`POST /api/nodes/{id}/cameras/{cam}/webrtc/offer`, `GET /api/node-stream/config`).
- Registers the reverse command tunnel proxy (`NewNodeProxyApi` at `/api/nodes/{id}/proxy/...`).
- The `ShutdownFunc` cancels the background context (stops heartbeat, control server, and media relay server).

## `ingestNodeEvent`

Maps a node-pushed event frame to the control plane's notification feed:
- `"notification"` — re-published as-is (re-tagged with `nodeId` and a new parent-side ID) via `republishNodeNotification`.
- `"going-offline"` — converted to a system warning notification.
- Any other kind (`health`, `disk-full`, `alert`, `system`, …) is no longer dropped: the frame is parsed as a `notification.Notification` when it carries a `Title`/`Body` (category/severity filled in via `categoryForNodeKind`/`severityForNodeKind` if unset), otherwise it is wrapped in a generic message (`"Node <kind> event"`, body truncated to 500 chars) tagged with the raw kind — so a node reporting trouble is never silently lost.
- `categoryForNodeKind` buckets by substring match: `health`/`disk`/`cert` → `CategoryHealthCheck`, `alert` → `CategoryVisionAlert`, else `CategorySystem`. `severityForNodeKind` similarly guesses `Critical` (`alert`/`full`/`critical`/`fail`), `Warning` (`health`/`warn`/`disk`), else `Info`.

## `publishFleetEvent`

Turns a `services.FleetEvent` detected by the registry's heartbeat reconciler into a `notification.Notification` (category `CategoryHealthCheck`, source `"node:<id>"`, `Data["nodeId"]` set) published to the unified feed:
- `FleetEventNodeLost` → `Critical` "Node offline" — unreachable past the grace window, marked lost.
- `FleetEventNodeRecovered` → `Info` "Node back online".
- `FleetEventCertExpiring` → `Warning` "Node certificate expiring", body humanized via `humanizeHours` (e.g. "about 2d 3h" / "about 6 hours"), or "has expired" when `HoursLeft <= 0`; `Data` also carries `certExpiresAt` and `hoursLeft`.

## Notes

- The accessrbac middleware (`deps.Access`) only gates myseliasan's own management endpoints; the node tunnel (`/api/nodes/{id}/proxy/...`) is authorized by a separate axis (per-node `NodeAccessGrant` + owner role). The node media API (`/api/nodes/{id}/cameras/{cam}/webrtc/offer`) uses the same per-node access grant axis.
- Opening `/` without a valid myseliasan session redirects to `/api/auth/start`.
- myidsan remains the SSO identity provider; myseliasan issues its own local session after code exchange.
- `pairing.controlPort` (default 49533) and `pairing.mediaPort` (default 49534) are both distinct from `pairing.mtlsPort` (49532). All three ports must be reachable from nodes.
- The media relay goroutine fetches the TLS config asynchronously (`registry.ParentServerTLS`) — if the fleet CA has not yet initialized, a warning is logged and the goroutine exits without starting the listener (the node's media channel will reconnect once the parent is enrolled).
- `nodeMedia` routes are registered before the proxy catch-all so their exact paths win over the prefix match.
