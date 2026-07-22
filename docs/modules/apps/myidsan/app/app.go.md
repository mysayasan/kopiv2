# Module: apps/myidsan/app/app.go

## Purpose

Implements the `myidsan` app module for the shared runtime host.

## Responsibilities

- Provides app identity and base directory.
- Registers identity, app-registry, app-auth-config, app-redirect-uri, user-session, endpoint, log, file-storage, operation-job, the shared `AccessRole`/`AccessRolePermission` entities, `SsoCa`, and the new `DirectoryConfig`/`FederatedGroupMapping` entities (LDAP/AD login, see `entities/directory_config.go.md`, `entities/federated_group_mapping.go.md`) for bootstrap schema generation.
- Registers built-in identity seeders and config-driven seed statements, including new menu-metadata seed rows for `/api/directory-config` (Federation → Directory, order 45) and `/api/federated-group-mapping` (no menu of its own — managed inline from the Directory page).
- Seeds the default `system` group. The stock superadmin account is no longer seeded via SQL; it is created/refreshed from `localAuth.username`/`localAuth.password` at startup by `EnsureStockSuperadmin` (see services/user_login.go), with `MustChangePassword = true`.
- Endpoint catalog is now app-local: myidsan no longer seeds endpoint rows for other apps. Legacy cross-app rows (`app_code <> 'myidsan'`) are deleted from `api_endpoint` on startup.
- **Relying apps are NOT auto-registered.** Following the standard OAuth / Google-console model, `myidsan`, `mymatasan`, `myseliasan`, and any other app can only obtain an authorization code once an operator has explicitly registered it under "Apps": an `app_registry` row, an `app_auth_config` (client ID + client secret), and at least one `app_redirect_uri`. Until then the federated auth flow rejects it with "client is not registered" (see `federated_auth.go` `loadClient`/`validateRedirectURI`). This is deliberate — auto-seeding these rows would let an unregistered app keep working after a database drop, closing a security hole. Previous behavior (auto-seeding `myidsan`/`mymatasan`/`myseliasan` app rows, SSO client, and redirect URIs on every startup) has been removed.
- Seeds wildcard-host app-scoped endpoint rows for the identity-management and RBAC-management APIs, including the shared `/api/access-rbac` prefix seeded as `DevOnly`.
- On first run, calls `EnsureStockSuperadmin` to seed (or refresh) the bootstrap account pinned to the accessrbac `superadmin` role.
- Binds the `user_login` table as the `AccessUserResolver` for `deps.Access` (`userLoginResolver` maps `UserRoleId → AccessPrincipal.RoleId`, `!IsActive → Disabled`, `MustChangePassword → MustChangePassword`). The resolver now uses `repo.GetById` (primary key) instead of the previous `GetByUnique(ctx,"","id",id)`, which matched no field and always returned the first user row — a critical auth bug causing every session to resolve as the stock superadmin's role.
- Registers myidsan-local login (including the authenticated change-password endpoint), LDAP/Active Directory login, Kerberos SPNEGO SSO, user, group, SSO fallback (introspect only), browser federated-auth, app-auth-config, app-redirect-uri, directory-config + federated-group-mapping, SSO certificate-authority, and identity-status handlers. `apis.NewLoginApi` now returns the `*login.Registry` it builds from `deps.Config.Login`, which is threaded straight into `apis.NewFederatedAuthApi` so the server-rendered login page, the SPA's `/api/login/providers` list, and the `/api/login/{provider}` routes all read from the same provider set (see `apis/login.go.md`, `infra/login/provider.go.md`).
- Wires up LDAP/AD login (`docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md` Phase 1): resolves the at-rest secret cipher via the new `openSecretCipher` (below), builds `services.NewDirectoryService` over the `DirectoryConfig`/`FederatedGroupMapping` repos, and threads it — together with a new shared `sharedapis.LoginGuard` (built by `loginGuardConfig`, below) — into `apis.NewLoginApi`, `apis.NewFederatedAuthApi`, and the new `apis.NewDirectoryConfigApi`. Registers `deps.Metrics.Describe(apis.MetricFederatedLoginTotal, ...)` for the LDAP (and, as of Phase 2, Kerberos) outcome counter.
- Wires up Kerberos SPNEGO SSO (`docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md` Phase 2), gated on `deps.Config.Kerberos.Enabled`: builds a `logininfra.KerberosAuthenticator` from `deps.Config.Kerberos.{KeytabPath, ServicePrincipal, OnlyRealms}`. **A bad or missing keytab logs a `WARNING` and leaves `kerberosAuth` nil** (Kerberos degrades to "not offered") rather than failing boot — mirroring how a half-configured OAuth provider is silently skipped. `deps.Config.Kerberos.DisplayLabel` (defaulting to `"Windows (SSO)"` when blank) is threaded into both `apis.NewLoginApi` (via the new `apis.LoginApiOptions{Kerberos, KerberosLabel, ...}`) and `apis.NewFederatedAuthApi`'s new `kerberosLabel` parameter, so the SSO button's label is consistent across both login surfaces — and absent from both when Kerberos isn't configured or fails to load.
- Provides OpenAPI metadata and descriptions for the identity and RBAC administration surface.

## `openSecretCipher`

Resolves the at-rest master key for myidsan's stored secrets (today: the directory
bind password) — the same `infra/atrest` boot sequence myseliasan's fleet secrets and
mymatasan's media encryption use (`atrest.OpenForStartup`, `security.encryptAtRest`
default true, `security.keyPath` default `<data>/secret/atrest.key`,
`security.keyProtector`/passphrase options, `security.recoveryPath`). Returns `nil`
(no encryption; the bind password is then stored as-is) when
`security.encryptAtRest` is explicitly `false`. A key that existed here before but is
now missing **fails closed** — `RegisterAppRoutes` returns an error and myidsan
refuses to boot, rather than minting a fresh key and silently orphaning the encrypted
bind password. This is new: myidsan had no at-rest-encrypted secret before the
directory bind password.

## `loginGuardConfig`

Maps the shared `deps.Config.LoginSecurity` config block (`enabled`, `maxAttempts`,
`windowSeconds`, `lockoutSeconds`, `lockoutMaxSeconds`, `failedDelayMs` — the same
block `mymatasan`/`myiotsan` already used) onto `sharedapis.LoginGuardConfig`. The
resulting `*sharedapis.LoginGuard` is shared by every interactive credential surface
in this app (local JSON login, LDAP JSON login, the server-rendered federated login
page's POST) — **new for myidsan**, which previously had no failed-login lockout at
all on any surface.

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
