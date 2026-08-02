# Module: apps/myiotsan/app/app.go

## Purpose

Implements the `myiotsan` app for the shared runtime host. `myiotsan` is the suite's IoT
sensor hub — "the NVR, but for sensors" — an appliance like mymatasan (single binary, on-prem,
air-gapped, adopted into the myseliasan fleet), reusing mymatasan's spine
(`device -> signal -> detector -> rule -> alert -> notify -> historize -> dashboard`); cameras
are one signal type, sensors are another. See `docs/MYIOTSAN_PLAN.md`.

**P0-P2 (the MVP) + P3 (discovery & onboarding) + P4 (actuation & device twin) + P6 (fleet),
shipped 2026-07-14:** the app boots, authenticates, ingests telemetry from real devices over an
embedded MQTT broker, evaluates rules against it, raises alerts, onboards unknown devices through
a time-boxed enrollment window rather than requiring every device to be provisioned by hand, and
— for the one profile that declares it (`smart-relay`) — can command a device: switch a
relay or set a setpoint, gated read-only-by-default/admin-only/declared-commands-only/
server-side-bounds/rate-limited/audited, and never auto-retried (see
`services/commands.go.md`). **It is also now an adoptable `myseliasan` fleet node** — see
"Fleet (P6)" below and `apps/myiotsan/app/wire_fleet.go.md`. **P5 (industrial protocols) has now
partially landed (2026-07-15): the Modbus/SunSpec driver foundation is WIRED IN** — a
`services.ModbusPoller` (`services/modbus_poller.go.md`) dials out to Modbus devices on their
profile's cadence and feeds `Ingest.HandlePolled`, and the shipped catalog gained its first two
POLLED profiles (`generic-sunspec-solar`, `huawei-sun2000`). **Guarded Modbus control writes
shipped 2026-07-16**, and the catalog then gained three more register-map profiles
(`sungrow-sh-hybrid`, `deye-hybrid`, `eastron-sdm630-meter`), a driver enhancement (fn 4 input
registers + float32 decoding, `infra/iot/modbus/regmap.go.md`), five more built-in solar Flow
Engine templates, and a new in-app knowledge base (`/api/kb`) — see the "P5" note below. What
remains of P5 is RTU (serial) and OPC-UA transports; the solar "system workspace" (P8) is still
design-only. See `docs/MYIOTSAN_PLAN.md` §8g. **A tabbed Settings page shipped 2026-07-16**,
consolidating users/roles, site location, outbound notification delivery (webhook/telegram — now
actually wired to the shared notification hub for the first time), storage/broker settings, fleet
pairing, and a restart control into one admin-only page — see the "Notes" section below. **A visual
executable Flow Engine (Node-RED-style canvas) shipped 2026-07-16, P1-P3** — see step 10e below,
`services/flow_runtime.go.md`, and `docs/MYIOTSAN_PLAN.md` §8i; this deliberately REVERSES §8g's
original "no visual node-graph editor" scope line, kept safe because every flow's actuation still
routes through the one guarded `CommandService.Issue` chokepoint.

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
  candidate table, (P4) `ProfileCommand`, `DeviceCommand`, `DeviceAttribute` — the actuation
  declaration, the command audit trail, and the device twin, (home-automation) `Scene`,
  `SceneAction`, `Schedule`, and (Flow Engine) `IotFlow` — the saved, executable data-flow graph
  authored on the visual canvas (`apps/myiotsan/entities`).
- `Seeders(...)` seeds the endpoint catalog for rate limiting/runtime metadata, now including
  `/api/devices`, `/api/profiles`, `/api/rules`, `/api/alerts`, `/api/notifications`, (P3)
  `/api/discovery` (auth-only), (P4) `/api/settings` (auth-only — users and roles; see the
  gap this closes below), `/api/kb` (auth-only — the shipped setup guides, see below), and
  `/api/setup` (auth-only — first-run setup state and completion, see below), alongside the
  original `/api/health`, `/api/version` (public), `/api/auth/login` (public),
  `/api/auth` (auth-only).
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
     then `apis.NewSettingsApi(protected, localUser, deps.AccessRoles, notificationSettings,
     telemetrySettings)` — user and role management (P4; **closes a real gap**: the policy
     catalog has named `/api/settings/users`/`/api/settings/roles` since P0, and the roles have
     existed since then, but nothing served them — viewer and operator were UNASSIGNABLE, and the
     appliance was effectively single-admin), plus, new with the tabbed Settings page, outbound
     notification delivery and telemetry/broker settings. See `apis/settings.go.md`. Immediately
     after, `apis.NewSystemApi(protected, deps.Restarter)` registers `POST /api/system/restart` —
     the Settings > System tab's restart, needed because the telemetry settings below are read
     once at boot (see `apis/system.go.md`). Immediately after that,
     `sharedservices.NewSetupStateService(dbsql.NewGenericRepo[sharedentities.RuntimeSetting](deps.Db))`
     and `apis.NewSetupApi(protected, setupStateService)` register the first-run wizard's
     completion flag — promoted off a `localStorage` key onto the same shared `setup.state`
     seam mymatasan/myidsan/myseliasan use, so dismissal is per-install rather than
     per-browser (see `apis/setup.go.md`, `domain/shared/services/setup_state.go.md`).
  8a. **Wires the two new runtime-editable settings stores, before the ingest spine is built**
     (so their effective values can feed it): `services.NewNotificationSettingsService(deps.Db,
     notificationService)`, then immediately `notificationSettings.Sync(ctx)` — applies any
     previously-saved webhook/telegram config to this run's fresh notification hub (a sync
     failure only logs a warning; it must not abort boot). **This Sync call is the reason
     myiotsan can deliver outside the app at all** — before this file existed, nothing ever called
     `notificationService.Configure`, so every alert stayed in the in-app feed no matter what an
     operator wanted. Then `services.NewTelemetrySettingsService(deps.Db, services.TelemetrySettings{...})`,
     seeded from `appCfg.Telemetry`/`appCfg.MQTT.Addr` as defaults, followed by
     `telemetrySettings.Get(ctx)` (`effTelemetry`) — **the effective, store-over-config values**
     that now feed `NewReadingWriter`'s batch/flush/queue sizing, `telemetry.RunRollup`'s
     retention, and `iotmqtt.New`'s listen address, replacing the direct `appCfg.Telemetry`/
     `appCfg.MQTT.Addr` reads those three call sites used before. A failure to load telemetry
     settings aborts startup (`fmt.Errorf("load telemetry settings: %w", err)`) rather than
     silently falling back, since a wrong broker address here means every device fails to
     connect. See `services/notification_settings.go.md` and `services/telemetry_settings.go.md`.
  9. **Wires the ingest spine** in dependency order (each stage owns the one before it):
     `broker -> ingest (decode -> deadband -> batched write) -> rules -> alert -> notification`.
     `services.NewDeviceService` (also the broker's `Authenticator`), `services.NewProfileService`
     + `EnsureBuiltins` (seeds the shipped device catalog; existing profiles are left alone so a
     site's tuned deadbands survive a restart), `services.NewTelemetryService`,
     `services.NewDeadbandGate`, `services.NewReadingWriter` (batch/flush/queue sized from
     `effTelemetry`, the store-over-config values resolved in 8a — not `appCfg.Telemetry`
     directly, since a saved edit must win over the shipped config) then `.Run(bgCtx)`,
     `services.NewRuleEngine` +
     `services.NewRuleService` then `.Reload(ctx)` (**re-seeds every cooldown from the alert
     log** — skipping this re-arms every still-true rule on every restart, the alert storm
     mymatasan shipped), `services.NewIngest`, then `iotmqtt.New` (the embedded broker, refuses
     to build with no authenticator) run via `safego.Go`.
  9a. **Wires runtime metrics** immediately after the ingest spine: `services.DescribeMetrics(deps.Metrics)`
      then `services.RunMetricsSampler(bgCtx, deps.Metrics, ingest, deviceService, 10*time.Second)`.
      Nine series total (was zero — a live scrape confirmed it), all instrumenting failure modes
      the ingest pipeline otherwise raises no error for: a dropped reading, a mistuned deadband, a
      device gone quiet, a failed/refused command. See `services/metrics.go.md`.
  9b. **Wires onboarding (P3)** immediately after the ingest spine: `services.NewEnrollment(deps.Db,
      profileService, logf)`, where `logf` both logs and publishes a `notification.CategorySystem`
      warning through `notificationService.Publish` — opening a window is a security event, and it
      must not be possible to do it quietly. Then `deviceService.SetEnrollment(enrollment)` and
      `ingest.SetEnrollment(enrollment)` wire the window into the authenticator and the hot path —
      see `services/enrollment.go.md` for why an unknown device presenting a device table that does
      not contain it is otherwise refused outright, and what the window's quarantine buys.
  9c. **Wires active network discovery scanning**, right after the enrollment wiring:
      `services.NewScanService(deps.Db, profileService, audit, logf)`, where `audit` publishes a
      `notification.CategorySystem`/Info event through `notificationService.Publish` — a scan is
      audited the same way opening the enrollment window is. `scanService` is then passed into
      `apis.NewDiscoveryApi` alongside `enrollment`/`deviceService` (step 11) so `POST
      /api/discovery/scan` can run it. See `services/scanner.go.md` and `infra/iot/discover`'s
      module docs; this is the counterpart to the announce path that feeds the identical
      quarantined `DiscoveredDevice` candidate list.
  10. Starts `telemetry.RunRollup(bgCtx, ...)` (rollup before purge — see `telemetry.go.md`) and
      a `safego.Supervise`d offline sweep (`ruleService.SweepOffline`) on a 1-minute
      `offlineSweepInterval` — the only way an "absence of readings" rule can ever fire, since a
      silent device never calls `Handle` again.
  10b. **Wires actuation (P4)**: `services.NewCommandService(deps.Db, deviceService,
      broker.Publish, audit, deps.Metrics, logf)` — `audit` publishes every attempt, INCLUDING every refusal,
      as a `notification.CategorySystem`/`Warning` notification ("somebody tried to unlock the
      front door at 03:00 and was refused" must not be thrown away just because it failed).
      `ingest.SetTwin(commandService)` wires the twin's reported half into the ingest hot path
      (see `services/ingest.go.md`). A `safego.Supervise`d sweep on a 10-second
      `commandSweepInterval` calls `commandService.SweepUnconfirmed(ctx)` — deliberately much
      more frequent than the offline sweep, because an operator staring at a "sent" command needs
      to be told promptly that it was never confirmed; a stale "in progress" is how somebody comes
      to believe a door is locked when it is not. See `services/commands.go.md` for every gate
      and why a command is never auto-retried.
  10c. **Wires the Modbus poller (P5)**, right after the command sweep and just before the API
      registrations: `services.NewModbusPoller(deviceService, profileService, ingest, logf)` run
      via `safego.Supervise` on `modbusReconcileInterval` (30s). This is the POLLED counterpart to
      the broker's PUSH path — a Modbus device does not publish, so something has to dial out to
      it on a schedule, then feed the identical `Ingest.HandlePolled` back half a broker message
      takes. See `services/modbus_poller.go.md`.
  10d. **Wires home automation (scenes + schedules)**, right after the Modbus poller:
      `services.NewSceneService(deps.Db, commandService, logf)` — a scene fans its ordered actions
      out through `commandService.Issue`, so running one commands nothing a single manual command
      could not; it is convenience, not a new authority. Then `services.NewScheduleService(deps.Db,
      sceneService, commandService, logf)` — fires a scene or a single command on the clock, or at
      sunrise/sunset ± an offset, through the identical actuation path, with the firing actor a
      synthetic `"schedule:<name>"` (id 0) so the audit trail attributes it rather than reading
      "System". A `safego.Supervise`d `"myiotsan.scheduler"` task (const `schedulerInterval` = 1
      minute) aligns its first tick to the next whole-minute boundary, then calls
      `scheduleService.Tick(ctx, time.Now())` once a minute — minute granularity matches the
      `LastFiredAt` double-fire guard (`services/schedules.go.md`) and the resolution a home
      schedule ("07:30") is actually expressed at. See `services/scenes.go.md` and
      `services/schedules.go.md`.
  10e. **Wires the Flow Engine**, right after home automation: `services.NewFlowService(deps.Db,
      logf)`, then `flowService.EnsureBuiltins(ctx)` seeds the shipped "Solar system" sample
      (`services/flow_catalog.go.md`). `services.NewFlowRuntime(flowService, commandService,
      notificationService, deviceService, writer, broker.Publish, logf)` builds the runtime — the
      `broker.Publish` param (P4) is the seam an `mqtt_out` output node uses to publish outward to
      the embedded broker; `flowService.SetOnChange(flowRuntime.SignalReload)` wires save/enable/
      delete to trigger an immediate recompile; `ingest.SetFlows(flowRuntime)` taps the SAME
      decoded-sample stream the rules do (see `services/ingest.go.md`); a `safego.Supervise`d
      `"myiotsan.flows"` task runs `flowRuntime.Run(ctx, flowReconcileInterval)` (const, 30s — the
      same reconcile cadence the Modbus poller uses). A flow's nodes can run ARBITRARY sandboxed
      JavaScript (`services/flow_eval.go.md`), but only a dedicated `command` output node can
      actuate, and it routes through `commandService.Issue` — the identical guarded chokepoint every
      other actuation path in this app uses, so a flow can command nothing a person could not; an
      `mqtt_out` output publishes data, not a command, so it does not go through that gate. See
      `services/flow_runtime.go.md` and `docs/MYIOTSAN_PLAN.md` §8i.
  11. Registers `apis.NewDevicesApi`, `apis.NewDiscoveryApi` (P3, now also taking `scanService`
      for the active-scan route — step 9c), `apis.NewCommandsApi` (P4),
      `apis.NewProfilesApi`, `apis.NewRulesApi`, `apis.NewScenesApi`, `apis.NewSchedulesApi`
      (home automation), `apis.NewFlowsApi` (Flow Engine, admin-only), `apis.NewKbApi` (the in-app
      knowledge base — reference content compiled into the binary via `go:embed`, granted to
      viewer/operator too since it is read-only; see `apps/myiotsan/kb/kb.go.md`),
      `apis.NewNotificationsApi` on `protected`.
  11b. **Wires the fleet (P6)**, gated on `deps.Config.Pairing.Enabled`: resolves
      `openFleetSecretCipher(deps)` (fails closed — see "Fleet (P6)" below), builds the fleet
      via `buildFleet(api, deps, appVersion(m), fleetCipher, notificationService)`
      (`wire_fleet.go.md`), registers the PUBLIC pairing routes
      (`sharedapis.NewPairingPublicApi`) on the **unauthenticated** router — before the
      protected subrouter, or the auth middleware would swallow the adopt call and the node
      could never be adopted — and the protected pairing routes
      (`sharedapis.NewPairingApi`) on `protected`, then calls `f.start(bgCtx, deps)`.
  12. The returned shutdown func cancels `bgCtx` then calls `writer.Wait(5*time.Second)` so a
      clean shutdown does not throw away readings the batcher already accepted.

## Fleet (P6)

`myiotsan` is adopted by `myseliasan` exactly the way `mymatasan` is, on the shared node stack
(`domain/shared/fleetnode`). It reports `fleetnode.KindIot`, so the control plane's fleet UI
and its correlator both know a sensor hub is not a camera node — see
`docs/modules/apps/myseliasan/services/node_registry.go.md` and
`docs/modules/apps/myseliasan/services/correlate.go.md`.

The event sink `buildFleet` registers is the line that makes the fourth app worth building:
every notification this node raises also lands in the control plane's unified feed via the
node-dialed control channel, and once `myseliasan` holds events from BOTH camera nodes and
sensor nodes it can correlate across them — motion on a camera AND a door opening AND no
badge swipe. Neither node can see that alone. See `apps/myseliasan/services/correlate.go.md`
for the correlator itself.

**A silent wiring miss, caught by live-booting, not by tests.** The 11b block above was
initially never inserted into `RegisterAppRoutes` — a string-replace edit missed it. The build
was green and every unit test passed, because nothing in the test suite exercised the actual
route table; the node simply had no pairing routes at all. It was caught only by a `404` on
`/api/pairing/fleet-key` when three real apps (myseliasan + an adopted mymatasan node + an
adopted myiotsan node) were booted together for a live end-to-end check. See
`docs/MYIOTSAN_PLAN.md` §8e for the full verification.
- `loginGuardConfig(deps)` maps `deps.Config.LoginSecurity.Effective()` onto `sharedapis.LoginGuardConfig`
  — identical shape to mymatasan's own mapping. Reading through `.Effective()` (rather than the
  struct fields directly) is what makes an absent `loginSecurity` block resolve to the guard
  being ON by default — see `infra/config/config_models.go.md`.
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

- **Runtime metrics (was 0 series on `/metrics` → 9):** a live scrape before this change showed
  myiotsan exposing nothing app-specific. The ingest pipeline is deliberately arranged so a
  publish never touches the database and so never raises an error a human sees — a dropped
  reading, a mistuned deadband, a device gone quiet are all silent without a scrape. See
  `services/metrics.go.md` for the full catalog; the headline is `myiotsan_ingest_dropped_total`
  (readings shed because the write queue was full), verified live at `dropped_total 86` against a
  torrent that outran the disk.
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
  P3 in `docs/MYIOTSAN_PLAN.md`, were deliberately NOT built at the time: MQTT sensors announce
  over MQTT, not mDNS, so a network scan would find gateways rather than sensors; the Modbus scan
  belonged with the Modbus poller in P5, where it could be tested against an actual device. The
  frontend gained a Discovery page and a first-run onboarding wizard
  (`views/react-webpack/src/views/components/discovery.js`, `.../onboarding.js`) that lead with
  enrollment as the primary onboarding path.
- **Active network discovery scanning shipped 2026-07-17** (see step 9c above): once the
  Modbus/SunSpec driver existed to test against, all four deferred scanners were built —
  `infra/iot/discover` (`ScanModbus`/`ScanMDNS`/`ScanSSDP`/`ScanEtherNetIP`/`ScanBACnet`, every
  scanner LAN-local/read-only/bounded/cancellable) plus `services.ScanService`
  (`POST /api/discovery/scan`, admin-only) feed the SAME quarantined `DiscoveredDevice` candidate
  table the announce path already wrote — a scan never adds a device, only proposes candidates
  to adopt. The Modbus scanner reuses `infra/iot/sunspec.Discover` to auto-identify a device
  (suggesting `generic-sunspec-solar`) or fall back to an "unidentified Modbus" candidate; a
  Modbus-scan candidate's endpoint/unit/transport now carry through adoption
  (`DiscoveredDevice.Endpoint`/`Unit`/`Transport`, `Enrollment.Adopt`) so it polls immediately.
  Verified live end to end for Modbus/mDNS/SSDP (scan → SunSpec-identify → candidate → adopt →
  device; mDNS/SSDP executed against a real LAN without error); EtherNet/IP and BACnet are
  **parser-verified only** — their `ListIdentity`/`Who-Is` reply decoders are tested against
  synthetic protocol-mock byte frames, since no real PLC was available in CI. See
  `docs/DISCOVERY_SCANNING.md` for the full phase table, safety posture, and what remains
  deliberately deferred (OPC-UA discovery, Profinet DCP, a Matter controller, native TV/AV
  control) and why. Not to be confused with `infra/discovery` (mymatasan's older camera
  ssdp/mdns/portscan discovery) — a separate package for a separate device family.
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
- **P5 (industrial protocols, app integration landed 2026-07-15)**: the Modbus/SunSpec driver
  foundation (`infra/iot/modbus`, `infra/iot/sunspec`, `tools/sunspec-sim`) that shipped 2026-07-15
  as a standalone, unwired foundation is now driven from this app. `entities.TelemetryKey` gained
  a Modbus binding (`Register`/`RegKind`/`ScaleFactor`/`WordSwap`); `entities.DeviceProfile` gained
  `Transport`/`ModbusMode`/`ModbusBase`/`PollSeconds`; `entities.IotDevice` gained `Endpoint`/`Unit`
  for a device the app dials OUT to rather than one that dials in. `services.Ingest.Handle` was
  split so its back half (`handleSamples`) is reusable, and a new `HandlePolled` entry point feeds
  it from a POLL driver with no payload to parse and no enrollment quarantine to apply — a polled
  device is one the operator configured, not a stranger that dialled in. `services.ModbusPoller`
  (new, `services/modbus_poller.go.md`) is the per-device poll-goroutine service, reconciled
  against the inventory on a ticker so a Modbus device added/edited/disabled in the UI is picked up
  live. The catalog gained its first two POLLED profiles: `generic-sunspec-solar` (self-describing,
  reads any compliant inverter/meter/battery with no per-model map) and `huawei-sun2000` (the
  vendor-register worked example for the world's most-installed inverter, whose battery and meter
  blocks sit far enough from its inverter block that `infra/iot/modbus.RegisterMap.Read` had to
  gain **clustered reads** — bounded per-block requests instead of one span the Modbus 125-register
  limit forbids). Verified live against `tools/sunspec-sim`'s new unit-4 Huawei persona: correctly
  scaled/signed readings stored end to end (49.99 Hz, 13.2% SOC, +600 W grid import).
  **Guarded Modbus control writes shipped 2026-07-16** (`entities.ProfileCommand` gained
  `Transport`/`Register`/`RegKind`/`ScaleFactor`; `CommandService.sendModbus` writes and
  read-back-confirms, single-register `u16`/`i16` only, never retried — `services/commands.go.md`).
  **Three more register-map profiles, a driver enhancement, five solar flow templates, and an
  in-app knowledge base shipped 2026-07-16** (branch `feat/myiotsan-solar-samples`):
  `sungrow-sh-hybrid` (INPUT registers + word-swapped 32-bit values, and the first built-in profile
  to pre-declare Modbus commands — `ems_mode`/`batt_force`/`batt_force_power`/`export_limit`/
  `export_limit_enable`/`batt_min_soc`/`batt_max_soc`, every one inert until an admin enables the
  device's actuation and bench-verifies the register), `deye-hybrid` (Deye/Sunsynk/Sol-Ark,
  all-holding, `work_mode`/`solar_sell`/`grid_charge`/`max_sell_power` commands), and
  `eastron-sdm630-meter` (read-only, float32 over input registers — the first profile needing float
  decoding). `infra/iot/modbus/regmap.go` gained an optional `inputReader` interface + `Point.Input`
  (`clusters()` now partitions by bank first, since fn 3/fn 4 can never share a round trip) and a
  `PF32` `PType` (`math.Float32frombits`) — both additive and backward-compatible (`Input` defaults
  `false`, so every existing holding-only map/reader is unchanged). `entities.TelemetryKey` gained
  `RegInput` and `"f32"` became a valid `RegKind`, threaded through `registerMapFromKeys`/`ptypeOf`
  and the profile CRUD/import-export/seed paths. Five new built-in Flow Engine templates
  (`services/flow_catalog.go.md`) ride the `$inverter` slot: derived self-consumption/
  self-sufficiency series, a low-SoC alert + force-charge guard, a grid export-limit control (the
  control showcase), and an overheat/fault alert — every command node among them stays inert for the
  same reason. A new in-app knowledge base (`apps/myiotsan/kb`, `go:embed`, `GET /api/kb`/
  `/api/kb/{slug}`, `apps/myiotsan/apis/kb.go`, readable by viewer/operator — `services/rbac.go.md`)
  ships eight compiled-in Markdown setup articles under `kb/solar/`; the frontend gained a Help page
  (`components/kb.js`). Verified live: the KB served, all profiles/flows seeded, and the
  FC04/f32/word-swap read path decoded correctly against a Modbus mock. Still outstanding: RTU
  (serial) and OPC-UA transports, and the solar "system workspace" (P8) — see
  `docs/MYIOTSAN_PLAN.md` §8g.
- **Home automation, Phases 1-3 (richer command kinds, scenes, schedules), shipped 2026-07-15:**
  moves `myiotsan` from "read sensors, actuate a relay" toward driving lamps/blinds/thermostats
  and grouping/scheduling those commands — see `docs/MYIOTSAN_PLAN.md` §8h for the full writeup.
  Phase 1 adds five `ProfileCommand` kinds beyond `switch`/`setpoint` (`dimmer`/`position`/`cct`/
  `mode`/`color`) and closes a real gap along the way: `validateValue`'s `switch` gained a
  `default` case that REFUSES an unrecognised `Kind` — before this change an unknown/misconfigured
  kind was published **unvalidated**. Phase 2 adds `Scene`/`SceneAction` (a named, ordered group of
  device commands) and `services.SceneService.Run`, which fans out through
  `commandService.Issue` per action — every gate still applies, partial failure is first-class
  (a scene never rolls back and never stops early), and a scene is convenience, not a new
  authority. Phase 3 adds `Schedule` (clock, or sunrise/sunset ± an offset via a pure NOAA
  calculation in `services/sun.go`, no network) and the `"myiotsan.scheduler"` minute tick above;
  a schedule fires through the identical actuation path with a synthetic `"schedule:<name>"`
  actor. **Phase 4 (rule-driven actuation — an `iot_rule` triggering a scene/command
  automatically) is explicitly OUT OF SCOPE here and deferred to a later, security-reviewed PR**:
  a rule that can WRITE to a device on its own is a materially different risk than a rule that
  only raises an alert (see `docs/MYIOTSAN_PLAN.md` §9's "scope creep into a Home Assistant clone"
  risk), and every safety property this app has built — read-only-by-default, admin-only,
  server-side bounds, rate limit, never-auto-retry — needs deliberate re-examination before
  something with no human in the loop can trigger it. Verified live end to end: seeded
  `smart-lamp`, issued a `dimmer` command, ran a scene (whose per-action report included a
  rate-limit refusal), set the site location, and created and test-fired a sunset schedule.
- **Tabbed Settings page, shipped 2026-07-16:** collapses everything that configures the hub
  itself (as opposed to the devices it watches) into one 6-tab page — users, location,
  notifications, telemetry, connectivity (fleet pairing), system — behind a single admin-only
  nav entry (`views/components/settings.js`). The two tabs new in this change are the ones with
  real backend behavior: **notifications** wires `services.NotificationSettingsService`
  (`services/notification_settings.go.md`) to `notification.Service.Configure` — before this,
  myiotsan had no code path that ever called `Configure`, so every alert stayed in the in-app feed
  no matter what an operator wanted; saving (or booting with a previously-saved config, via
  `Sync`) is what makes an alert reach a webhook or telegram at all. **Telemetry** wires
  `services.TelemetrySettingsService` (`services/telemetry_settings.go.md`), a store-over-config
  blob for retention days/write-batcher sizing/broker address that `app.go` now reads once at
  boot (`effTelemetry`) instead of reading `appCfg.Telemetry`/`appCfg.MQTT.Addr` directly — an
  edit here takes effect only after a restart, which is why this change also added
  `apis.NewSystemApi`'s `POST /api/system/restart` (`apis/system.go.md`). Along the way, the RBAC
  catalog (`services/rbac.go.md`) gained explicit admin-only rows for
  `/api/settings/notification`, `/api/settings/telemetry`, `/api/system`, and — a gap that
  predated this change and was simply never caught — `/api/pairing`, which had never been listed
  in the catalog at all. Verified live end to end: created a user and assigned it a role, set the
  site location, saved and test-fired a webhook (confirming `Configure` reached the live hub),
  saved telemetry retention (confirming defaults are preserved for unset fields), read pairing
  status, and read version/health.
