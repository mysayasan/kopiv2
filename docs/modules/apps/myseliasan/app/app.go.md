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
- Builds `INodeRegistry` and registers node-management routes (`NewNodesApi`).
- Starts a background heartbeat goroutine to probe adopted nodes over mTLS.
- Builds the unified notification service and registers `NewNotificationApi`.
- Starts the control channel server (`ControlServer`) on a dedicated fleet-mTLS port (`pairing.controlPort`, default `49533`) to accept node-dialed bi-directional connections. Node-pushed events are ingested into the notification feed via `ingestNodeEvent`.
- Registers per-node access-grant management (`NewNodeAccessApi`).
- Registers the reverse command tunnel proxy (`NewNodeProxyApi` at `/api/nodes/{id}/proxy/...`).
- The `ShutdownFunc` cancels the background context (stops heartbeat + control server).

## `ingestNodeEvent`

Maps a node-pushed event frame to the control plane's notification feed:
- `"notification"` — re-published as-is (re-tagged with `nodeId` and a new parent-side ID).
- `"going-offline"` — converted to a system warning notification.

## Notes

- The accessrbac middleware (`deps.Access`) only gates myseliasan's own management endpoints; the node tunnel (`/api/nodes/{id}/proxy/...`) is authorized by a separate axis (per-node `NodeAccessGrant` + owner role).
- Opening `/` without a valid myseliasan session redirects to `/api/auth/start`.
- myidsan remains the SSO identity provider; myseliasan issues its own local session after code exchange.
- The fleet-CA, pairing, and mTLS wiring is unchanged from the LAN discovery / adoption epic.
- `pairing.controlPort` is distinct from `pairing.mtlsPort` (49532): the control channel server listens on 49533 by default.
