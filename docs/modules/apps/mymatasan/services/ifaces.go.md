# Module: apps/mymatasan/services/ifaces.go

## Purpose

Declares service contracts for app-specific domain.

## Interfaces

- `IOnvifDeviceService`
  - `Discover(ctx, timeoutMs)` for WS-Discovery scans
  - `Probe(ctx, address)` for manual IP or device-service URL checks
  - CRUD/upsert operations for saved ONVIF device entities
  - `ResolveStream(ctx, id, credentials)` for ONVIF media-service RTSP URI resolution
  - `TestStream(ctx, id)` for RTSP DESCRIBE/SETUP probing
- `IRuntimeSettingsService`
  - `Get(ctx)` and `Save(ctx, settings)` for SQLite-backed runtime settings
  - `Reset(ctx)` to restore startup config defaults
  - `Stream(ctx)` and `Decoder(ctx)` for focused runtime reads
- `ILocalUserService`
  - `EnsureDefaultAdmin(ctx)` seeds the first standalone admin account
  - `Authenticate(ctx, username, password)` validates Basic Auth credentials
  - CRUD and password reset operations for Settings user management
- `IVisionService`
  - `GetRules(ctx, limit, offset)` and `SaveRule(ctx, req, userId)` for detection rule management
  - `DeleteRule(ctx, id)` for removing stale rules
  - `GetAlerts(ctx, limit, offset)` and `CreateAlert(ctx, req, userId)` for alert event management
  - `AcknowledgeAlert(ctx, id, userId)` for operator acknowledgement

## Why It Matters

- Keeps handlers and service implementations loosely coupled.
- Allows swapping/testing ONVIF and RTSP concrete implementations.
- Keeps local login independent from MyIDSan identity/RBAC services.
- Keeps AI rule and alert APIs behind reusable vision contracts instead of binding detector logic to handlers.
