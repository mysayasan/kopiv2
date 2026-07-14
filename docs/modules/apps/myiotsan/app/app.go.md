# Module: apps/myiotsan/app/app.go

## Purpose

Implements the `myiotsan` app for the shared runtime host. `myiotsan` is the suite's IoT
sensor hub — "the NVR, but for sensors" — an appliance like mymatasan (single binary, on-prem,
air-gapped, adopted into the myseliasan fleet), reusing mymatasan's spine
(`device -> signal -> detector -> rule -> alert -> notify -> historize -> dashboard`); cameras
are one signal type, sensors are another. See `docs/MYIOTSAN_PLAN.md`.

**This file is P0: the scaffolding.** The app boots, authenticates, and serves its SPA shell.
The IoT domain — devices, profiles, telemetry ingest, rules — lands in P1/P2 and is
deliberately absent rather than stubbed, so nothing here is a placeholder pretending to work.

## Key Type: module

An empty `struct{}` — myiotsan holds no cross-request state in P0 (unlike, say, myseliasan's
`module`, which caches listener handles for `ReadinessStatus`).

## Responsibilities

- `Name()` → `"myiotsan"`; `BaseDir()` → `apps/myiotsan`.
- `SharedAPIs()` trims the shared API surface to what an appliance actually needs: disables
  `AppRegistry`, `ApiEndpoint`, `FileStorage`, `CacheService` — myiotsan is a single-tenant
  device on someone's LAN, not a platform with a registry or file-storage service to
  administer. Mirrors mymatasan's own trimmed surface.
- `Entities()` is the P0 schema: everything needed to sign a user in and record what
  happened — `ApiEndpoint`, `ApiLog`, `UserSession`, `Notification`, and the **shared**
  appliance user/role entities (`LocalUser`, `AccessRole`, `AccessRolePermission` from
  `domain/entities`, the same types mymatasan uses). The IoT domain tables (`iot_device`,
  `device_profile`, `telemetry_key`, `device_reading`, `reading_rollup`, `iot_rule`,
  `alert_event`) arrive with the code that uses them in P1/P2.
- `Seeders(...)` seeds the P0 endpoint catalog for rate limiting/runtime metadata: `/api/health`,
  `/api/version` (public), `/api/auth/login` (public), `/api/auth` (auth-only — the session
  probe + change-password group).
- `RegisterAppRoutes(api, deps)`:
  1. Builds `sharedservices.NewLocalUserService` on a `LocalUser` repo bound to `deps.Db`.
  2. Seeds roles **before** the admin is seeded (`services.EnsureRoles` — myiotsan's own
     catalog wrapper over `sharedservices.EnsureApplianceRoles`; see
     `apps/myiotsan/services/rbac.go.md`), then resolves the superadmin role id.
  3. `localUser.EnsureDefaultAdmin(ctx, deps.Config.LocalAuth.Username/Password)` seeds the
     bootstrap admin; on `Seeded`, calls `announceFirstRunAdmin` (`firstrun.go.md`) — the
     console banner + recovery file is the only place a CLI/Docker/systemd operator learns the
     credential.
  4. Builds the notification store (`notification.NewService`) **in P0** so security events
     (a sign-in lockout) are recorded from the first boot, even though there is no read API
     for them yet — an event never written cannot be shown later once P1 adds the feed.
  5. Builds `sharedapis.LocalAuthConfig{AppName: "myiotsan", OnLockout: ...}`, wiring
     `OnLockout` directly to `notificationService.Publish` (myiotsan has no separate
     `NotifyAuthLockout` helper the way mymatasan does — it publishes the
     `notification.Notification` inline).
  6. Registers `sharedapis.NewLocalLoginApi` on the **public** `api` router — must be mounted
     before the protected subrouter, since it is the endpoint that authenticates.
  7. Mounts `protected := api.PathPrefix("").Subrouter()` with, in order,
     `sharedapis.NewLocalBasicAuth` then `sharedapis.NewRequireRolePermission` — order is
     load-bearing: auth puts the principal in context, and the matrix needs a principal to
     decide against.
  8. Registers `sharedapis.NewLocalAuthApi` (session probe + change-password) on `protected`.
- `loginGuardConfig(deps)` maps `deps.Config.LoginSecurity` onto `sharedapis.LoginGuardConfig`
  — identical shape to mymatasan's own mapping.
- `RegisterWebRoutes` serves `index.html` from `deps.HomeDir` (not `BaseDir()` — see the
  `myseliasan`/`mymatasan` note on why: a packaged install runs with the binary and `static/`
  side by side and a working directory pointed elsewhere), with
  `Cache-Control: no-cache, no-store, must-revalidate` since it points at content-hashed
  chunks that must never be served stale after a rebuild.
- `APIDocs()` returns the Swagger metadata + the P0 endpoint set (`/api/auth/login`,
  `/api/auth/logout`, `/api/auth/session`, `/api/auth/change-password`). `docVersion` reads
  the embedded version manifest (`versioning.LoadDefault().InfoForApp("myiotsan")`), falling
  back to the literal `"0.1.0"` if that lookup fails.

## Notes

- Uses `sharedapis.NewLocalLoginApi` (session-cookie login, new with this app — see
  `domain/shared/apis/local_login_api.go.md`) as its primary sign-in path rather than
  Basic-only; Basic still works for API clients since `NewLocalBasicAuth` accepts both.
- The shared appliance local-auth stack (`domain/shared/apis`, `domain/shared/services`) is
  what makes this file short: bcrypt handling, session comparison, the auth-verification
  cache, and the role/permission mechanics are all reused from mymatasan's extraction, not
  reimplemented here.
