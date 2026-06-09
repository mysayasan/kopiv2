# Module: apps/mymatasan/app/app.go

## Purpose

Implements the `mymatasan` app module for the shared runtime host.

## Responsibilities

- Provides app identity and base directory.
- Registers app entities for bootstrap schema generation.
- Registers built-in and config-driven seeders.
- Wires app-specific APIs (`onvif`, `settings`, `vision`).
- Mounts app-specific APIs behind standalone DB-backed local Basic Auth.
- Seeds the first local admin user (`admin` / `Admin123`) when no local users exist.
- Owns the app-local stream manager used by WebRTC live view and closes it during graceful shutdown.
- Wires SQLite-backed runtime settings seeded from `decoder` and `stream` config defaults.
- Starts the app-local vision monitor worker for enabled AI detection rules.
- Provides API docs metadata and endpoint descriptions for shared Swagger/OpenAPI output.
- Uses the embedded app version as the OpenAPI info version when available.

## Notes

- Only the public shared version API is mounted for this standalone app.
- Shared login, user/group/role, app-registry, endpoint, endpoint-RBAC, file-storage, log, runtime-log, and cache-service route groups are disabled.
- Shared entity registration keeps only the endpoint metadata and API log tables needed by apphost middleware.
- App entity registration includes `OnvifDevice`, `RuntimeSetting`, `LocalUser`, `DetectionRule`, and `AlertEvent`.
- OpenAPI endpoint discovery is automatic; this module enriches summaries/descriptions via `APIDocs()`.
- Built-in endpoint seeds set `appCode=mymatasan` and `accessTier` values (`1=AuthOnly`, `2=Public`) for local rate-limit classification.
- The module does not seed identity or RBAC rows.
- API docs metadata includes the public runtime version endpoint (`GET /api/version`).
- API docs metadata includes ONVIF discovery, manual probe, save, list, delete, runtime settings, local users, stream config, stream URI resolution, camera password change, PTZ move/stop, RTSP test, WebRTC live view, MJPEG fallback, vision rule, and vision alert endpoints.
- This file is the main extension point to add app-local behavior while keeping startup generic.
