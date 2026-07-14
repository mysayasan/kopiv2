# Module: apps/myiotsan/app/app.go

## Purpose

Implements the `myiotsan` app for the shared runtime host. `myiotsan` is the suite's IoT
sensor hub — "the NVR, but for sensors" — an appliance like mymatasan (single binary, on-prem,
air-gapped, adopted into the myseliasan fleet), reusing mymatasan's spine
(`device -> signal -> detector -> rule -> alert -> notify -> historize -> dashboard`); cameras
are one signal type, sensors are another. See `docs/MYIOTSAN_PLAN.md`.

**P0-P2 (the MVP) + P3 (discovery & onboarding) + P4 (actuation & device twin), shipped
2026-07-14:** the app boots, authenticates, ingests telemetry from real devices over an embedded
MQTT broker, evaluates rules against it, raises alerts, onboards unknown devices through a
time-boxed enrollment window rather than requiring every device to be provisioned by hand, and
now — for the one profile that declares it (`smart-relay`) — can command a device: switch a
relay or set a setpoint, gated read-only-by-default/admin-only/declared-commands-only/
server-side-bounds/rate-limited/audited, and never auto-retried (see
`services/commands.go.md`). What remains is industrial protocols (P5) and fleet adoption (P6).

## Key Type: module

`module` now carries `cfg *iotconfig.Config` — myiotsan's own slice of `config.json` (the
`mqtt` and `telemetry_store` blocks), decoded through the phase-C `apphost.AppConfigDecoder`
seam rather than added to the shared `AppConfigModel` every other app would then carry.
`DecodeAppConfig` is the host's call-in; `appConfig()` defaults it (via `iotconfig.Load(nil)`)
for the rare case — a test constructing the module directly — where the host never called the
decoder. This is a change from P0, where `module` was an empty `struct{}`.

## Responsibilities

- `Name()` → `"myiotsan"`; `BaseDir()` → `apps/myiotsan`.
- `SharedAPIs()` trims the shared API surface to what an appliance actually needs: disables
  `AppRegistry`, `ApiEndpoint`, `FileStorage`, `CacheService` — myiotsan is a single-tenant
  device on someone's LAN, not a platform with a registry or file-storage service to
  administer. Mirrors mymatasan's own trimmed surface.
- `Entities()` — the shared appliance schema (`ApiEndpoint`, `ApiLog`, `UserSession`,
  `Notification`, `LocalUser`, `AccessRole`, `AccessRolePermission`) plus the IoT domain now
  registered here: `DeviceProfile`, `TelemetryKey`, `IotDevice`, `DeviceReading`,
  `ReadingRollup`, `IotRule`, `AlertEvent`, (P3) `DiscoveredDevice` — the enrollment window's
  candidate table, and (P4) `ProfileCommand`, `DeviceCommand`, `DeviceAttribute` — the actuation
  declaration, the command audit trail, and the device twin (`apps/myiotsan/entities`).
- `Seeders(...)` seeds the endpoint catalog for rate limiting/runtime metadata, now including
  `/api/devices`, `/api/profiles`, `/api/rules`, `/api/alerts`, `/api/notifications`, (P3)
  `/api/discovery` (auth-only), and (P4) `/api/settings` (auth-only — users and roles; see the
  gap this closes below), alongside the original `/api/health`, `/api/version` (public),
  `/api/auth/login` (public), `/api/auth` (auth-only).
- `RegisterAppRoutes(api, deps)`:
  1. Builds `sharedservices.NewLocalUserService` on a `LocalUser` repo bound to `deps.Db`.
  2. Seeds roles **before** the admin is seeded (`services.EnsureRoles` — myiotsan's own
     catalog wrapper over `sharedservices.EnsureApplianceRoles`; see
     `apps/myiotsan/services/rbac.go.md`), then resolves the superadmin role id.
  3. `localUser.EnsureDefaultAdmin(ctx, deps.Config.LocalAuth.Username/Password)` seeds the
     bootstrap admin; on `Seeded`, calls `announceFirstRunAdmin` (`firstrun.go.md`) — the
     console banner + recovery file is the only place a CLI/Docker/systemd operator learns the
     credential.
  4. Builds the notification store (`notification.NewService`). It is now the **unified feed**:
     rule alerts (`CategoryDeviceAlert`), device health, and the app's own security events (a
     sign-in lockout) all land here, giving an operator one place to look.
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
  8. Registers `sharedapis.NewLocalAuthApi` (session probe + change-password) on `protected`,
     then (P4) `apis.NewSettingsApi(protected, localUser, deps.AccessRoles)` — user and role
     management. **Closes a real gap**: the policy catalog has named `/api/settings/users`/
     `/api/settings/roles` since P0, and the roles have existed since then, but nothing served
     them — viewer and operator were UNASSIGNABLE, and the appliance was effectively
     single-admin. See `apis/settings.go.md`.
  9. **Wires the ingest spine** in dependency order (each stage owns the one before it):
     `broker -> ingest (decode -> deadband -> batched write) -> rules -> alert -> notification`.
     `services.NewDeviceService` (also the broker's `Authenticator`), `services.NewProfileService`
     + `EnsureBuiltins` (seeds the shipped device catalog; existing profiles are left alone so a
     site's tuned deadbands survive a restart), `services.NewTelemetryService`,
     `services.NewDeadbandGate`, `services.NewReadingWriter` (batch/flush/queue sized from
     `appCfg.Telemetry`) then `.Run(bgCtx)`, `services.NewRuleEngine` +
     `services.NewRuleService` then `.Reload(ctx)` (**re-seeds every cooldown from the alert
     log** — skipping this re-arms every still-true rule on every restart, the alert storm
     mymatasan shipped), `services.NewIngest`, then `iotmqtt.New` (the embedded broker, refuses
     to build with no authenticator) run via `safego.Go`.
  9b. **Wires onboarding (P3)** immediately after the ingest spine: `services.NewEnrollment(deps.Db,
      profileService, logf)`, where `logf` both logs and publishes a `notification.CategorySystem`
      warning through `notificationService.Publish` — opening a window is a security event, and it
      must not be possible to do it quietly. Then `deviceService.SetEnrollment(enrollment)` and
      `ingest.SetEnrollment(enrollment)` wire the window into the authenticator and the hot path —
      see `services/enrollment.go.md` for why an unknown device presenting a device table that does
      not contain it is otherwise refused outright, and what the window's quarantine buys.
  10. Starts `telemetry.RunRollup(bgCtx, ...)` (rollup before purge — see `telemetry.go.md`) and
      a `safego.Supervise`d offline sweep (`ruleService.SweepOffline`) on a 1-minute
      `offlineSweepInterval` — the only way an "absence of readings" rule can ever fire, since a
      silent device never calls `Handle` again.
  10b. **Wires actuation (P4)**: `services.NewCommandService(deps.Db, deviceService,
      broker.Publish, audit, logf)` — `audit` publishes every attempt, INCLUDING every refusal,
      as a `notification.CategorySystem`/`Warning` notification ("somebody tried to unlock the
      front door at 03:00 and was refused" must not be thrown away just because it failed).
      `ingest.SetTwin(commandService)` wires the twin's reported half into the ingest hot path
      (see `services/ingest.go.md`). A `safego.Supervise`d sweep on a 10-second
      `commandSweepInterval` calls `commandService.SweepUnconfirmed(ctx)` — deliberately much
      more frequent than the offline sweep, because an operator staring at a "sent" command needs
      to be told promptly that it was never confirmed; a stale "in progress" is how somebody comes
      to believe a door is locked when it is not. See `services/commands.go.md` for every gate
      and why a command is never auto-retried.
  11. Registers `apis.NewDevicesApi`, `apis.NewDiscoveryApi` (P3), `apis.NewCommandsApi` (P4),
      `apis.NewProfilesApi`, `apis.NewRulesApi`, `apis.NewNotificationsApi` on `protected`.
  12. The returned shutdown func cancels `bgCtx` then calls `writer.Wait(5*time.Second)` so a
      clean shutdown does not throw away readings the batcher already accepted.
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
- **SQLite write throughput — the risk `docs/MYIOTSAN_PLAN.md` §9 called the one thing that
  could invalidate the storage design — is measured and settled.** On a live appliance, 20
  devices publishing 10,000 MQTT payloads (~30,000 samples) in under a second produced 540
  written rows, 98.2% suppressed by the deadband, zero dropped. Do not add a TSDB; it would
  break the single-binary deployment model and is not needed. `GET /api/devices/stats`
  (`services.IngestStats`) exposes stored/suppressed/dropped so this stays observable in
  production — a non-zero, growing `dropped` means the disk has stopped keeping up.
- Rules are evaluated on **every** decoded sample, including ones the deadband suppressed — see
  `services/rules.go.md`. The deadband is a storage decision, not a detection one.
- **P3 (discovery & onboarding, shipped 2026-07-14):** an admin opens a time-boxed enrollment
  window (`/api/discovery/window`); an unknown device presenting the window's key is admitted
  QUARANTINED (`infra/iot/mqtt.Principal.Enrolling`) rather than refused outright — its payloads
  become `DiscoveredDevice` candidates and nothing else, no telemetry, no rule evaluation. The
  admin reviews candidates (each carrying a profile suggestion scored off its observed payload
  keys) and adopts or rejects them. mDNS/SSDP/portscan and a Modbus TCP scan, also listed under
  P3 in `docs/MYIOTSAN_PLAN.md`, were deliberately NOT built: MQTT sensors announce over MQTT,
  not mDNS, so a network scan would find gateways rather than sensors; the Modbus scan belongs
  with the Modbus poller in P5, where it can be tested against an actual device. The frontend
  gained a Discovery page and a first-run onboarding wizard
  (`views/react-webpack/src/views/components/discovery.js`, `.../onboarding.js`) that lead with
  enrollment as the primary onboarding path.
- **P4 (actuation & device twin, shipped 2026-07-14):** a device can be commanded — switch a
  relay, set a setpoint — but only for the one shipped profile that declares any command
  (`smart-relay`; every other profile in the catalog remains read-only). Every gate `docs/MYIOTSAN_PLAN.md`
  §3.4 asked for is enforced server-side in `services.CommandService`
  (`services/commands.go.md`): read-only by default (`IotDevice.ActuationEnabled`), admin-only
  (`services/rbac.go.md`, a rule written in P0 before the command path existed), only what the
  profile declares (no generic publish-to-any-topic endpoint), server-side bounds, a 2s
  per-device rate limit, and a `device_command` audit row for every attempt including refusals.
  **A command is never auto-retried** — re-sending a relay write is a second physical action, so
  an unconfirmed command becomes `failed` after 30s (`SweepUnconfirmed`, swept every 10s) rather
  than being resent; verified live with a relay simulator that obeys but never reports back, the
  relay physically switched, the command failed, and exactly one command was ever sent. The
  device twin (`DeviceAttribute`, desired vs reported) does **not** re-apply an expired desire
  (5-minute TTL) when a device reconnects — the obvious twin implementation would, and for a door
  controller that is dangerous (a month-old "unlock" applying itself when the device finally comes
  back online). §3.4 said every command would be written to `myseliasan`'s existing `audit_log`;
  that did **not** ship, because myiotsan is a standalone appliance that may never be adopted into
  a fleet — its audit trail is its own `device_command` table plus the notification feed instead;
  see `docs/MYIOTSAN_PLAN.md` §8d for the deviation. P4 also closed a real gap unrelated to
  actuation itself: `/api/settings/users`/`/api/settings/roles` had been named in the policy
  catalog since P0 but were never served, making viewer/operator unassignable — `apis.NewSettingsApi`
  (P4) now serves them on the shared appliance user service. See `apis/commands.go.md`,
  `apis/settings.go.md`, `entities/device_command.go.md`, `entities/profile_command.go.md`,
  `entities/device_attribute.go.md`.
