# Module: apps/myidsan/app/app.go

## Purpose

Implements the `myidsan` app module for the shared runtime host.

## Responsibilities

- Provides app identity and base directory.
- Registers identity, app-registry, app-auth-config, app-redirect-uri, user-session, endpoint, log, file-storage, operation-job, and the shared `AccessRole`/`AccessRolePermission` entities for bootstrap schema generation.
- Registers built-in identity seeders and config-driven seed statements.
- Seeds the default `system` group and `superadmin` login with bcrypt password storage.
- Seeds wildcard-host app-scoped endpoint rows for the identity-management and RBAC-management APIs, including the shared `/api/access-rbac` prefix seeded as `DevOnly`.
- Seeds registered app rows for `myidsan`, `mymatasan`, and `myseliasan`.
- Seeds MySeliaSan client auth config and exact callback URI defaults for development.
- On first run, re-points the bootstrap `superadmin` login's `UserRoleId` to the accessrbac `superadmin` role (seeded by apphost) so that account bypasses the permission matrix.
- Binds the `user_login` table as the `AccessUserResolver` for `deps.Access` (`userLoginResolver` maps `UserRoleId → AccessPrincipal.RoleId`, `!IsActive → Disabled`).
- Registers myidsan-local login, user, group, SSO fallback (introspect only), browser federated-auth, app-auth-config, and app-redirect-uri handlers.
- Provides OpenAPI metadata and descriptions for the identity and RBAC administration surface.

## Removed (accessrbac migration)

- The `UserRole` entity and its service/DTO/API (`user_role.go`) are deleted. Role management is now the shared accessrbac surface at `/api/access-rbac`.
- `POST /api/sso/authorize` is removed. Authorization decisions no longer go through myidsan; each app's accessrbac middleware enforces the matrix locally.
- `deps.Rbac` and `SharedAPIConfig.ApiEndpointRbac` are removed from the shared wiring.
- Per-endpoint RBAC seed rows are no longer inserted; the bootstrap `superadmin` bypasses the matrix entirely.

## Notes

- Shared operational APIs (logs, file storage, cache, runtime log, endpoint catalog, and the new accessrbac roles+permissions management) are mounted by `infra/apphost`.
- Redis is the preferred cache provider for multi-app deployments; memory cache remains process-local and can use the myidsan `POST /api/sso/introspect` service-to-service fallback.
