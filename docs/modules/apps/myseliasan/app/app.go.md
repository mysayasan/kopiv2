# Module: apps/myseliasan/app/app.go

## Purpose

Implements the `myseliasan` relying control-plane app for the shared runtime host.

## Responsibilities

- Provides app identity and base directory.
- Registers `ManagedNode` and `ControlSetting` entities for DB bootstrap.
- Disables shared management APIs except the public version endpoint.
- Seeds a local endpoint catalog for rate limiting and runtime metadata (includes `/api/nodes` as `AuthOnly` and `/api/nodes/self-dropped` as `Public`).
- Registers relying-app auth/session API routes.
- Builds `INodeRegistry` (via `NewNodeRegistry` with a `NodeRegistryConfig` that includes `MTLSPort`, `CertTTL`, and `HeartbeatInterval` from the `pairing.*` config) and registers node-management routes via `NewNodesApi`.
- Starts a background heartbeat goroutine that calls `INodeRegistry.Heartbeat` on the configured interval (`pairing.heartbeatIntervalSeconds`, default 60s). The goroutine is stopped via the `ShutdownFunc` returned from `RegisterAppRoutes`.
- Derives `parentID` from `sso.clientId` (falls back to `"myseliasan"`) and `parentBaseURL` from `sso.redirectBaseUrl`; these are stamped into every adoption call so nodes know their parent.
- Registers protected web routes for `/` and `/index.html` before static asset fallback.

## Notes

- MySeliaSan does not register user-management entities.
- Opening `/` without a valid MySeliaSan session redirects to `/api/auth/start`.
- MyIDSan remains the identity provider; MySeliaSan creates its own local session only after code exchange.
- The fleet-key multicast address is read from `pairing.multicastAddr`; empty defaults to the `infra/pairing` package default (`239.255.90.21:49531`).
- The heartbeat loop is distinct from the `monitorCtx` of `mymatasan`; it is controlled by a dedicated `context.WithCancel` and stopped by the `ShutdownFunc`.
- The `fleetCA` (inside `nodeRegistry`) is self-contained and never contacts `myidsan`; it stores the CA key material in the local `ControlSetting` table.
