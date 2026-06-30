# Module: apps/myidsan/app/app.go

## Purpose

Implements the `myidsan` app module for the shared runtime host.

## Responsibilities

- Provides app identity and base directory.
- Registers identity, app-registry, app-auth-config, app-redirect-uri, user-session, endpoint, log, file-storage, operation-job, the shared `AccessRole`/`AccessRolePermission` entities, and the new `SsoCa` entity for bootstrap schema generation.
- Registers built-in identity seeders and config-driven seed statements.
- Seeds the default `system` group. The stock superadmin account is no longer seeded via SQL; it is created/refreshed from `localAuth.username`/`localAuth.password` at startup by `EnsureStockSuperadmin` (see services/user_login.go), with `MustChangePassword = true`.
- Endpoint catalog is now app-local: myidsan no longer seeds endpoint rows for other apps. Legacy cross-app rows (`app_code <> 'myidsan'`) are deleted from `api_endpoint` on startup.
- **Relying apps are NOT auto-registered.** Following the standard OAuth / Google-console model, `myidsan`, `mymatasan`, `myseliasan`, and any other app can only obtain an authorization code once an operator has explicitly registered it under "Apps": an `app_registry` row, an `app_auth_config` (client ID + client secret), and at least one `app_redirect_uri`. Until then the federated auth flow rejects it with "client is not registered" (see `federated_auth.go` `loadClient`/`validateRedirectURI`). This is deliberate — auto-seeding these rows would let an unregistered app keep working after a database drop, closing a security hole. Previous behavior (auto-seeding `myidsan`/`mymatasan`/`myseliasan` app rows, SSO client, and redirect URIs on every startup) has been removed.
- Seeds wildcard-host app-scoped endpoint rows for the identity-management and RBAC-management APIs, including the shared `/api/access-rbac` prefix seeded as `DevOnly`.
- On first run, calls `EnsureStockSuperadmin` to seed (or refresh) the bootstrap account pinned to the accessrbac `superadmin` role.
- Binds the `user_login` table as the `AccessUserResolver` for `deps.Access` (`userLoginResolver` maps `UserRoleId → AccessPrincipal.RoleId`, `!IsActive → Disabled`, `MustChangePassword → MustChangePassword`). The resolver now uses `repo.GetById` (primary key) instead of the previous `GetByUnique(ctx,"","id",id)`, which matched no field and always returned the first user row — a critical auth bug causing every session to resolve as the stock superadmin's role.
- Registers myidsan-local login (including the new authenticated change-password endpoint), user, group, SSO fallback (introspect only), browser federated-auth, app-auth-config, app-redirect-uri, SSO certificate-authority, and identity-status handlers.
- Provides OpenAPI metadata and descriptions for the identity and RBAC administration surface.

## Removed (accessrbac migration)

- The `UserRole` entity and its service/DTO/API (`user_role.go`) are deleted. Role management is now the shared accessrbac surface at `/api/access-rbac`.
- `POST /api/sso/authorize` is removed. Authorization decisions no longer go through myidsan; each app's accessrbac middleware enforces the matrix locally.
- `deps.Rbac` and `SharedAPIConfig.ApiEndpointRbac` are removed from the shared wiring.
- Per-endpoint RBAC seed rows are no longer inserted; the bootstrap `superadmin` bypasses the matrix entirely.

## Notes

- Shared operational APIs (logs, file storage, cache, runtime log, endpoint catalog, and the new accessrbac roles+permissions management) are mounted by `infra/apphost`.
- Redis is the preferred cache provider for multi-app deployments; memory cache remains process-local and can use the myidsan `POST /api/sso/introspect` service-to-service fallback.
- The `localAuth` config block (`username`, `password`) drives the stock superadmin identity. Change both fields to match your deployment's initial credentials; they are only effective while `MustChangePassword` is still true on the account (i.e., before the operator has changed the password through the UI).
- mTLS enforcement on the token endpoint is not yet implemented (apphost TLS listener has no client-cert hook); `GET /api/sso-ca` and `POST /api/sso-ca/issue/{id}` are available for future use when token-endpoint mTLS is wired up.
