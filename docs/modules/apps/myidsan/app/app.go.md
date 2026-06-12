# Module: apps/myidsan/app/app.go

## Purpose

Implements the `myidsan` app module for the shared runtime host.

## Responsibilities

- Provides app identity and base directory.
- Registers identity, app-registry, app-auth-config, app-redirect-uri, user-session, endpoint, RBAC, log, file-storage, and operation-job entities for bootstrap schema generation.
- Registers built-in identity seeders and config-driven seed statements.
- Seeds the default `system` group, `superadmin` role, and `superadmin` login with bcrypt password storage.
- Seeds wildcard-host app-scoped endpoint and RBAC rows for identity-management APIs.
- Seeds registered app rows for `myidsan`, `mymatasan`, and `myseliasan`.
- Seeds MySeliaSan client auth config and exact callback URI defaults for development.
- Registers myidsan-local login, user, group, role, SSO fallback, browser federated-auth, app-auth-config, and app-redirect-uri handlers.
- Provides OpenAPI metadata and descriptions for the identity and RBAC administration surface.

## Notes

- Shared operational APIs are mounted by `infra/apphost`; myidsan owns and mounts identity APIs from `apps/myidsan/apis`.
- Redis is the preferred cache provider for multi-app deployments; memory cache remains process-local and can use the myidsan service-to-service fallback APIs.
