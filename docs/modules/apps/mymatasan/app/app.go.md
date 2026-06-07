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

- Shared APIs (login, user/group/role, endpoint, RBAC, file storage, logs, cache-service) are mounted by `infra/apphost`.
- Shared entity registration includes `OperationJob` so durable upload jobs are bootstrapped with the app schema.
- OpenAPI endpoint discovery is automatic; this module enriches summaries/descriptions via `APIDocs()`.
- Built-in core seed SQL is idempotent and portable for both Postgres and MariaDB engines.
- Built-in core seeds include the first-run `superadmin` login (`superadmin` / `superadmin123`) mapped to the `superadmin` role with the seeded password stored as bcrypt.
- API docs metadata now describes local auth endpoints (`POST /api/login/default`, `POST /api/login/default/register`) in addition to optional OAuth endpoints.
- API docs metadata includes the public runtime version endpoint (`GET /api/version`).
- API docs metadata includes file-storage sync upload, async upload, ID-based download, inline view, and job status endpoints.
- This file is the main extension point to add app-local behavior while keeping startup generic.
