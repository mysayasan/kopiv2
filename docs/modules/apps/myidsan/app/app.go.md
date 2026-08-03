# Module: apps/myidsan/app/app.go

## Purpose

Implements the `myidsan` app module for the shared runtime host.

## Responsibilities

- Provides app identity and base directory.
- Registers identity, app-registry, app-auth-config, app-redirect-uri, user-session, endpoint, log, file-storage, operation-job, the shared `AccessRole`/`AccessRolePermission`/`RuntimeSetting` entities, `SsoCa`, the `DirectoryConfig`/`FederatedGroupMapping` entities (LDAP/AD login, see `entities/directory_config.go.md`, `entities/federated_group_mapping.go.md`), the `UserMfaFactor`/`UserMfaRecoveryCode` entities (TOTP second factor, see `entities/user_mfa_factor.go.md`, `entities/user_mfa_recovery_code.go.md`), the `UserWebauthnCredential` entity (FIDO2 security keys, many rows per user — see `entities/user_webauthn_credential.go.md`), the `PasswordResetRequest` entity (account-recovery request queue, see `entities/password_reset_request.go.md`), the `UserAvatar` entity (profile picture, one row per account, see `entities/user_avatar.go.md`), and the `AuditLog` entity (append-only security trail, see `entities/audit_log.go.md`) for bootstrap schema generation. `RuntimeSetting` is new — it backs the first-run setup-wizard completion flag (see `domain/shared/services/setup_state.go.md`).
- Registers built-in identity seeders and config-driven seed statements, including new menu-metadata seed rows for `/api/directory-config` (Federation → Directory, order 45), `/api/federated-group-mapping` (no menu of its own — managed inline from the Directory page), and `/api/setup` (first-run setup state, `DevOnly`, no menu — the wizard is not a navigable page).
- Seeds the default `system` group. The stock superadmin account is no longer seeded via SQL; it is created/refreshed from `localAuth.username`/`localAuth.password` at startup by `EnsureStockSuperadmin` (see services/user_login.go.md), with `MustChangePassword = true`.
- **First-run/lock-out-recovery sequencing** (`RegisterAppRoutes`, see `app/firstrun.go.md`): before seeding, `consumeAdminResetMarker` checks for a `RESET_ADMIN` marker file in the data dir and, if present, deletes it and calls `ResetStockSuperadmin` instead of the normal `EnsureStockSuperadmin` path. Either way, when the resulting `StockSeedResult.Seeded` is true (a fresh account, or a reset), `announceFirstRunAdmin` prints a console banner and writes `INITIAL_ADMIN_LOGIN.txt` to the data dir.
- Constructs `sharedservices.NewSetupStateService` (`domain/shared/services`, the suite-wide seam extracted from what used to be a myidsan-local `services.NewSetupStateService` copy) over a `dbsql.NewGenericRepo[sharedentities.RuntimeSetting]` and wires it into `apis.NewSetupApi(api, *deps.Auth, deps.Access, ...)`, mounting `GET /api/setup/state` and `POST /api/setup/complete` (see `apis/setup.go.md`, `domain/shared/services/setup_state.go.md`). The wizard itself (`views/react-webpack/src/views/components/setup.js`) gained a 4th step, "Where sessions live" (`STEP_KEYS` 5 → 6), that reads `GET /api/settings/storage`, offers a live Redis connectivity test (`POST /api/settings/cache/test`), and saves via `PUT /api/settings/storage` by merging its `cache` block onto the live section payload — a naive rebuild would wipe the co-located `fileStorage` block.
- Endpoint catalog is now app-local: myidsan no longer seeds endpoint rows for other apps. Legacy cross-app rows (`app_code <> 'myidsan'`) are deleted from `api_endpoint` on startup.
- **Relying apps are NOT auto-registered.** Following the standard OAuth / Google-console model, `myidsan`, `mymatasan`, `myseliasan`, and any other app can only obtain an authorization code once an operator has explicitly registered it under "Apps": an `app_registry` row, an `app_auth_config` (client ID + client secret), and at least one `app_redirect_uri`. Until then the federated auth flow rejects it with "client is not registered" (see `federated_auth.go` `loadClient`/`validateRedirectURI`). This is deliberate — auto-seeding these rows would let an unregistered app keep working after a database drop, closing a security hole. Previous behavior (auto-seeding `myidsan`/`mymatasan`/`myseliasan` app rows, SSO client, and redirect URIs on every startup) has been removed.
- Seeds wildcard-host app-scoped endpoint rows for the identity-management and RBAC-management APIs, including the shared `/api/access-rbac` prefix seeded as `DevOnly`.
- On first run, calls `EnsureStockSuperadmin` to seed (or refresh) the bootstrap account pinned to the accessrbac `superadmin` role.
- Binds the `user_login` table as the `AccessUserResolver` for `deps.Access` (`userLoginResolver` maps `UserRoleId → AccessPrincipal.RoleId`, `!IsActive → Disabled`, `MustChangePassword → MustChangePassword`). The resolver now uses `repo.GetById` (primary key) instead of the previous `GetByUnique(ctx,"","id",id)`, which matched no field and always returned the first user row — a critical auth bug causing every session to resolve as the stock superadmin's role. `deps.Access.SetResolver` is now called **after** `mfaService` is constructed (Productization Phase 3), rather than immediately after the user login service, because the resolver needs it: `userLoginResolver` gained `mfa services.IMfaService` and `mfaPolicy config.EffectiveMfaPolicy` (from `deps.Config.Mfa.Effective()`) fields. `ResolveAccessUser` now also sets `AccessPrincipal.MustEnrollMfa`: when `mfaPolicy.RequiresFactor(u.UserRoleId, isDirectory)` is true (`isDirectory := u.SsoProvider != ""` — a directory/social account's factor policy usually belongs to its upstream provider, so it is only in scope when `mfa.applyToDirectory` says so), it calls `mfa.HasConfirmedFactor` and sets the flag to the inverse. A lookup error here **fails open** (logged, `MustEnrollMfa` left `false`) rather than locking every admin out of the product because one query failed — the login itself already succeeded; the worst case is a required factor not enforced for that one request. See `domain/shared/services/access_rbac.go.md`, `domain/utils/middlewares/access_rbac.go.md`, and `infra/config/config_models.go.md`'s `mfa` block.
- Resolves the password-strength policy once (`deps.Config.PasswordPolicy.Effective()`, Productization Phase 3 — see `infra/config/config_models.go.md`) and threads it into `services.NewUserLoginService` (new trailing `policy` parameter) and `apis.NewUserLoginApi` (new trailing `policy` parameter, inserted before `trustedProxies`), so admin-provisioned account creation (`POST /api/user-credential`) and every service-layer password-setting method enforce the same configured rules — see `services/password_policy.go.md`, `services/user_login.go.md`, `apis/user_login.go.md`.
- Registers myidsan-local login (including the authenticated change-password endpoint), LDAP/Active Directory login, Kerberos SPNEGO SSO, user, group, SSO fallback (introspect only), browser federated-auth, app-auth-config, app-redirect-uri, directory-config + federated-group-mapping, SSO certificate-authority, and identity-status handlers. `apis.NewLoginApi` now returns the `*login.Registry` it builds from `deps.Config.Login`, which is threaded straight into `apis.NewFederatedAuthApi` so the server-rendered login page, the SPA's `/api/login/providers` list, and the `/api/login/{provider}` routes all read from the same provider set (see `apis/login.go.md`, `infra/login/provider.go.md`).
- Wires up LDAP/AD login (`docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md` Phase 1): resolves the at-rest secret cipher via the new `openSecretCipher` (below), builds `services.NewDirectoryService` over the `DirectoryConfig`/`FederatedGroupMapping` repos, and threads it — together with a new shared `sharedapis.LoginGuard` (built by `loginGuardConfig`, below) — into `apis.NewLoginApi`, `apis.NewFederatedAuthApi`, and the new `apis.NewDirectoryConfigApi`. Registers `deps.Metrics.Describe(apis.MetricFederatedLoginTotal, ...)` for the LDAP (and, as of Phase 2, Kerberos) outcome counter.
- Wires up Kerberos SPNEGO SSO (`docs/MYIDSAN_ENTERPRISE_SSO_PLAN.md` Phase 2), gated on `deps.Config.Kerberos.Enabled`: builds a `logininfra.KerberosAuthenticator` from `deps.Config.Kerberos.{KeytabPath, ServicePrincipal, OnlyRealms}`. **A bad or missing keytab logs a `WARNING` and leaves `kerberosAuth` nil** (Kerberos degrades to "not offered") rather than failing boot — mirroring how a half-configured OAuth provider is silently skipped. `deps.Config.Kerberos.DisplayLabel` (defaulting to `"Windows (SSO)"` when blank) is threaded into both `apis.NewLoginApi` (via the new `apis.LoginApiOptions{Kerberos, KerberosLabel, ...}`) and `apis.NewFederatedAuthApi`'s new `kerberosLabel` parameter, so the SSO button's label is consistent across both login surfaces — and absent from both when Kerberos isn't configured or fails to load.
- Wires up TOTP second-factor authentication (`docs/MYIDSAN_MFA_PLAN.md`, now shipped): builds `services.NewMfaService(mfaFactorRepo, mfaRecoveryRepo, secretCipher, "myidsan")` — reusing the same `openSecretCipher` at-rest cipher as the directory bind password — **before** `apis.NewLoginApi` so the two password login paths can gate on it. Describes `MetricMfaChallengeTotal` via `deps.Metrics.Describe`. Threads `mfaService` into `apis.NewLoginApi` (`LoginApiOptions{Mfa, Store: deps.Cache}`), `apis.NewFederatedAuthApi` (new trailing `mfaService` parameter), and mounts the self-service/admin surface via `apis.NewMfaApi(api, *deps.Auth, deps.Access, mfaService, userLoginService, deps.Metrics)` (`/api/mfa/*`, `/api/mfa-admin/{id}` — see `apis/mfa.go.md`). Before any of this, calls `consumeMfaResetMarker` (see `app/firstrun.go.md`) — the `RESET_MFA` boot-marker escape hatch for a sole-superadmin lost-device lockout.
- Wires up account recovery ("forgot password"): builds a `mailer.New(mailer.Config{...})` from the new `deps.Config.Smtp` block (see `infra/config/config_models.go.md`) — `Enabled` defaults false, so an air-gapped install never reaches for a network — and `services.NewPasswordResetService(resetRequestRepo, userLoginService, resetMailer, deps.Cache)` (see `services/password_reset.go.md`). Logs a one-line startup notice when the mailer is actually enabled. Threads `passwordResetService` into `apis.NewLoginApi` (`LoginApiOptions.Reset`, the public `POST /api/login/forgot`) and `apis.NewFederatedAuthApi`'s new trailing `resetService` parameter (the server-rendered `/api/auth/forgot`/`/api/auth/reset`), and mounts the superadmin operator queue via `apis.NewPasswordResetApi(api, *deps.Auth, deps.Access, passwordResetService)` (`/api/password-reset`, see `apis/password_reset.go.md`). Seeds a menu row (`Id: "resetRequests"`, group `Identity`, `SeedRbac: true`) so the queue is matrix-gated like any other admin page.
- Mounts profile-picture avatars via `apis.NewProfileApi(api, *deps.Auth, deps.Access, avatarRepo)` (`/api/profile/avatar`, self-service + `/api/profile/avatar/{userId}`, superadmin — see `apis/profile.go.md`). No dedicated service layer; the API mounts a bare generic repo directly, unlike MFA/password-reset. Backs the consolidated self-service **Profile** page (change password + MFA + avatar, reached only from the account chip in the side rail — a `chipOnly` route, not a nav item) and the Users management page's per-row avatar/camera-button. `avatarRepo` is now built once and shared with `backupService` (below), since the avatar table is also one of the sections a backup exports/restores.
- Wires up **Backup & restore** (`docs/MYIDSAN_PRODUCTIZATION_PLAN.md` Phase 1): builds `services.NewBackupService` over every repo the six backup sections touch (roles/permissions, users/groups/avatars, MFA factors/recovery codes, app registry/auth config/redirect URIs, directory config/group mappings, SSO CA), the same `secretCipher` the directory/MFA services use (so TOTP secrets and the LDAP bind password can be unsealed on export and re-sealed on restore), `deps.Cache` (so a restore can drop every live session), `setupStateService`, and `moduleAppVersion(m)` (stamped into the backup manifest so a restore can warn on a version mismatch). Mounts `apis.NewBackupApi(api, *deps.Auth, deps.Access, backupService)` (`/api/backup/*`, superadmin-only regardless of matrix grants — see `apis/backup.go.md`, `services/backup.go.md`). Seeds a menu row (`Id: "backup"`, group `System`) **without** `SeedRbac` — deliberately never delegated by default, since an export is the whole identity store in one file.
- Wires up **Audit log, session administration, and step-up re-authentication**
  (`docs/MYIDSAN_PRODUCTIZATION_PLAN.md` Phase 2): builds `services.NewAuditService`
  (over a `dbsql.NewGenericRepo[myidsanentities.AuditLog]`, `log.Printf` diagnostics)
  before `apis.NewLoginApi`, since authentication events are the bulk of what it records,
  and passes it by value into every handler that performs a sensitive action rather than
  reaching it through a global. Builds `services.NewSessionService` (over
  `dbsql.NewGenericRepo[sharedentities.UserSession]`, `deps.Cache`) — the cache entry
  remains the authority on whether a session is valid; this table exists so sessions can
  be LISTED per user, which the cache alone cannot answer. Builds
  `services.NewStepUpService(userLoginService, mfaService, deps.Cache)`, a 5-minute
  cache-backed re-authentication marker gating the most dangerous of a 72-hour
  superadmin session's powers. All three are threaded into `apis.NewLoginApi`
  (`LoginApiOptions{Audit, Sessions, TrustedProxies: deps.Config.RateLimit.TrustedProxies}`,
  so login/session events share the rate limiter's trusted-proxy list), `apis.NewUserLoginApi`
  (trailing `sessions, audit, trustedProxies` — ending a disabled/deleted account's
  sessions), `apis.NewDirectoryConfigApi`, `apis.NewBackupApi`, `apis.NewMfaApi`, and
  `apis.NewPasswordResetApi` (each gaining trailing `audit`/`stepUp`/`trustedProxies`
  parameters where applicable), plus two new mounts: `apis.NewStepUpApi` (`/api/step-up`,
  auth-only) and `apis.NewSessionApi` (`/api/session` self-service auth-only, `/api/session-admin`
  superadmin) — see `apis/audit.go.md`, `apis/session.go.md`, `apis/stepup.go.md`,
  `services/audit.go.md`, `services/session.go.md`, `services/stepup.go.md`. `apis.NewAuditApi`
  (`/api/audit`, superadmin-only) is also mounted here. Seeds menu-metadata endpoint rows
  for `/api/step-up` (`AuthOnly`, no menu), `/api/session` (`AuthOnly`, no menu — reached
  from the Profile page, not a nav item), `/api/session-admin` (`DevOnly`, no menu — reached
  inline from the Users page), and `/api/audit` (`DevOnly`, menu `Id: "audit"`, group
  `System`, order 5) — `SeedRbac` is omitted on all four so none is delegated by default.
  Immediately after `auditService` is constructed, calls `startAuditRetention(deps,
  auditService)` (`app/audit_retention.go.md`, `docs/MYIDSAN_PRODUCTIZATION_PLAN.md` Phase 4)
  — a no-op unless `config.audit.retention.enabled` is set, in which case it schedules the
  age-based, archive-first trim of the trail as a periodic `deps.Scheduler` task.
- Wires up **IdP metrics** (`docs/MYIDSAN_PRODUCTIZATION_PLAN.md` §4.4, `services/metrics.go.md`):
  calls `services.DescribeMetrics(deps.Metrics)` before constructing `auditService`; attaches
  `deps.Metrics` to it via a type-asserted `WithMetrics` call (so `auditService`'s package-private
  concrete type does not have to be exported just to hand back the same `IAuditService`) right
  after construction, so `MetricAuditWriteFailuresTotal` is live before anything can call
  `Record`; and calls `startSessionGauge(deps, sessionService)`
  (`app/audit_retention.go.md`) right after `sessionService` is built, registering the
  `myidsan_sessions_active` polling task. `apis.NewFederatedAuthApi`'s call site gained a
  trailing `deps.Metrics` argument so `POST /api/auth/token` can record
  `MetricTokenExchangeTotal{outcome}` — see `apis/federated_auth.go.md`.
- Wires up the **in-app Settings editor** (`docs/MYIDSAN_PRODUCTIZATION_PLAN.md`
  §4.1, ported from myseliasan's pattern — see `services/settings.go.md`,
  `services/settings_apply.go.md`, `services/settings_materialize.go.md`,
  `apis/settings.go.md`): builds `services.NewSettingsService(deps.Config, deps.ConfigPath,
  deps.Db, secretCipher, logf)` and mounts `apis.NewSettingsApi(api, *deps.Auth, deps.Access,
  settingsService, auditService, deps.Config.RateLimit.TrustedProxies)` right after
  `apis.NewIdentityStatusApi`. Covers a safe subset of `config.json` — `localAuth`, `sso`,
  `security`, `storage`, `logging` — via `GET /api/settings`, `GET`/`PUT
  /api/settings/{section}`, `POST /api/settings/{section}/reset`,
  `POST /api/settings/cache/test`, gated by superadmin **middleware** on the whole route
  group (matching audit/backup/mfa-admin/session-admin) rather than myseliasan's per-handler
  wrapper. Deliberately excludes `audit.retention` (the trail's only removal path is
  config-file-only on purpose), `kerberos`, and `login.oidc[]`/social providers, and — unlike
  myseliasan — has no server-side file-browse endpoint, since myidsan is the identity
  provider. Seeds a `Settings` menu row (`Id: "settings"`, group `System`, order 90,
  `AccessTier: DevOnly`, no `SeedRbac`). **The React frontend page has since shipped**
  (`apps/myidsan/views/react-webpack/src/views/components/settings.js`) — a tabbed editor
  (one tab per section plus a System tab) with per-field (i) info tips, masked secret fields
  (a blank submission keeps the stored value), Save gated on a genuine diff, per-section
  Restore-defaults, and a "restart required" banner wired to the `system` API below.
- Mounts `apis.NewSystemApi(api, *deps.Auth, deps.Access, deps.Restarter)` (`/api/system`,
  see `apis/system.go.md`) right after the Settings API, superadmin-gated as middleware and
  never delegated through the matrix. It exists so the Settings editor's `needsRestart:
  true` has somewhere to send the operator: without it the page could report a restart was
  required and offer no way to perform one. Seeds an `/api/system` endpoint row
  (`AccessTier: AuthOnly`, no menu — reached only from inside the Settings page).
- Wires up **WebAuthn / FIDO2 security keys** as a second factor kind alongside TOTP (a user
  may hold either or both; the login gate accepts whichever is presented): resolves
  `deps.Config.WebAuthn.Effective()` and builds `services.NewWebAuthnService` over a
  `dbsql.NewGenericRepo[myidsanentities.UserWebauthnCredential]`, `deps.Cache` (ceremony
  state — a cache entry, not a table, mirroring the pre-session MFA challenge), and a
  `webauthninfra.Authority` (`infra/webauthn`, see `infra/webauthn/webauthn.go.md`) built
  from that policy. **No at-rest cipher is passed to the service, and that is not an
  omission** — a WebAuthn credential is a public key, so there is nothing to seal, unlike
  the sealed `UserMfaFactor.SecretEnc` next to it. Logs the resolved Relying Party ID (or
  `"(derived from the request host)"` when left empty) and the user-verification setting
  once at boot when the feature is enabled. `webauthnService` is threaded into
  `apis.NewLoginApi` (`LoginApiOptions.WebAuthn`, arming the two pre-session legs
  `POST /api/login/mfa/webauthn/{begin,finish}` alongside the existing TOTP challenge — see
  `apis/login.go.md`, `apis/mfa_challenge.go.md`), **`apis.NewFederatedAuthApi`'s new
  trailing `webauthnService` parameter** — this one closed a real MFA bypass found while
  wiring it up: the server-rendered `/api/auth/login` page (where every relying app's SSO
  hop lands, `myseliasan` included) had its `*mfaChallenger` built from `mfaService` alone,
  so an account whose *only* factor was a security key would have cleared that page's
  gate with **no factor checked at all**; see `apis/federated_auth.go.md`'s "Second-Factor
  (MFA) Login Challenge" section — `apis.NewUserLoginApi` (so deleting an
  account also removes its keys — an orphaned credential id would block re-enrolling that
  physical key, since `CredentialId` is unique across the whole table), `backupService`
  (below), and mounted for self-service + admin management via
  `apis.NewWebAuthnApi(api, *deps.Auth, deps.Access, webauthnService, userLoginService,
  mfaService, auditService, stepUpService, deps.Config.RateLimit.TrustedProxies)`
  (`/api/mfa/webauthn`, `/api/mfa-admin/{id}/webauthn` — see `apis/webauthn.go.md`,
  `services/webauthn.go.md`). Also registers `myidsanentities.UserWebauthnCredential{}` for
  bootstrap schema generation (see `entities/user_webauthn_credential.go.md`) and includes
  its repo in `services.NewBackupService`'s section-3 (`mfa`) wiring — see
  `services/backup.go.md`.
- `moduleAppVersion(m)` — a small helper factored out of `APIDocs()` (below) so both the OpenAPI metadata and the backup manifest read this app's released version from the shared version manifest the same way, falling back to `"1.0.0"` when the manifest is unreadable.
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

Maps the shared `deps.Config.LoginSecurity.Effective()` config block (`enabled`, `maxAttempts`,
`windowSeconds`, `lockoutSeconds`, `lockoutMaxSeconds`, `failedDelayMs` — the same
block `mymatasan`/`myiotsan` already used) onto `sharedapis.LoginGuardConfig`. Reading
through `.Effective()` rather than the struct fields directly is what makes an absent
`loginSecurity` block resolve to the guard being ON by default (see
`infra/config/config_models.go.md`) — previously an absent block silently decoded to
`Enabled=false`. The
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
