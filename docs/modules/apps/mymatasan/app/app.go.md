# Module: apps/mymatasan/app/app.go

## Purpose

Implements the `mymatasan` app module for the shared runtime host.

## Responsibilities

- Provides app identity and base directory.
- Registers app entities for bootstrap schema generation.
- Registers built-in and config-driven seeders.
- Wires app-specific APIs (`admin`, `home`, `camera`).
- Provides API docs metadata and endpoint descriptions for shared Swagger/OpenAPI output.
- Uses the embedded app version as the OpenAPI info version when available.
- Starts camera autostart workers and returns shutdown hook.

## Notes

- Shared APIs (file storage, logs, cache-service, version) are mounted by `infra/apphost`.
- Shared login, user/group/role, app-registry, endpoint, and endpoint-RBAC management APIs are disabled for this resource app.
- Shared entity registration includes `OperationJob` so durable upload jobs are bootstrapped with the app schema.
- Shared entity registration includes `AppRegistry` and `UserSession` for SSO-compatible schema.
- OpenAPI endpoint discovery is automatic; this module enriches summaries/descriptions via `APIDocs()`.
- Built-in core seed SQL is idempotent and portable for both Postgres and MariaDB engines.
- Built-in core seeds include the first-run `superadmin` login (`superadmin` / `superadmin123`) mapped to the `superadmin` role with the seeded password stored as bcrypt.
- Built-in endpoint seeds set `appCode=mymatasan` and `accessTier` values (`0=DevOnly`, `1=AuthOnly`, `2=Public`); protected shared management APIs are seeded as `DevOnly`.
- API docs metadata includes the public runtime version endpoint (`GET /api/version`).
- API docs metadata includes file-storage sync upload, async upload, ID-based download, inline view, and job status endpoints.
- This file is the main extension point to add app-local behavior while keeping startup generic.
