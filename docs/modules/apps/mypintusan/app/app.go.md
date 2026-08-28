# Module: apps/mypintusan/app/app.go

## Purpose

The `apphost.App` composition root for `mypintusan`, the suite's physical access-control
appliance — myidsan decides who signs into software, this decides who goes through a door. This
file is what makes the previously test-only `services.Controller`/`SQLStore`/decision path
(`services/controller.go.md`, `services/store_sql.go.md`) **bootable as a running service** for
the first time: it wires identity (local admin seed, login guard, session auth), RBAC role
seeding, the SQLStore, the alarm sink, the OSDP bus runtime (`app/runtime.go.md`), and the HTTP
API (`apis/`).

It is the first app in the suite whose failure mode is A PERSON TRAPPED BEHIND A DOOR, and that
shapes everything below it. Free egress is hardware — a mechanical lever, or a power-cut
interlock wired in series on a maglock feed — and nothing in this process can override it. A
panic in a Go goroutine must not be able to trap anybody in a stairwell.

## Key Type: module

```go
type module struct {
    cfg *pintuconfig.Config
}
```

Carries mypintusan's own config slice (`config/config.go.md`) decoded through
`apphost.AppConfigDecoder`, the same phase-C per-app seam myiotsan uses — the shared
`AppConfigModel` carries what every app needs; mypintusan's `access`/`buses` blocks stay
top-level in `config.json` rather than being added to the shared model every other app would
then carry.

## Responsibilities

- `Name()` → `"mypintusan"`; `BaseDir()` → `apps/mypintusan`.
- `DecodeAppConfig(raw, dataDir)` — calls `pintuconfig.Load(raw)`. `appConfig()` defaults it
  (`pintuconfig.Load(nil)`) for the rare case (a test constructing the module directly) where
  the host never called the decoder.
- `SharedAPIs()` trims the shared surface the way every appliance does: no `AppRegistry`, no
  `ApiEndpoint` service, no `FileStorage`, no `CacheService` — a door controller is a
  single-tenant box on a building's LAN, not a platform.
- `Entities()` — the shared appliance block (`ApiEndpoint`, `ApiLog`, `UserSession`,
  `Notification`, `LocalUser`, `AccessRole`, `AccessRolePermission`, `RuntimeSetting`) plus
  `services.Entities()` — the 12-table access-control schema (`services/schema.go.md`).
- `Seeders(seedStatements)` — seeds the `/api/doors`, `/api/readers`, `/api/holders`,
  `/api/events`, `/api/lockdown`, `/api/settings`, `/api/setup`, `/api/notifications` (the unified
  event feed: alarms, badge decisions, security events), `/api/groups`, `/api/grants`,
  `/api/schedules` (the access-rules triple — `apis/access_rules.go.md`), plus `/api/health`,
  `/api/version`, `/api/auth/login`, `/api/auth` endpoint catalog rows, the same insert-if-absent /
  repair-app_code-and-tier SQL pattern the other appliances use.
- `RegisterAppRoutes(api, deps)`:
  1. Builds `services.NewAccessSettingsService(store.SettingsRepo(), settingsFromConfig(cfg))` and
     calls `settings.Get(ctx)` — `config.json` **seeds** this call's first-ever write to the
     `runtime_setting` table; every call after that reads the database row and ignores
     `config.json` entirely (`services/runtime_settings.go.md`). This is mymatasan's
     seed-then-database-owns pattern, and it is what makes the app configurable by a facilities
     manager rather than by somebody editing JSON over SSH.
  2. Resolves the site timezone from the **live settings**, via `live.Location()`, and refuses to
     start if it is wrong — schedules and holidays are local concepts, and a silent fall back to
     UTC would shift every schedule on the site. This replaced a call to `cfg.Location()`
     (`config/config.go.md`) directly on `config.json`; see that file's Notes for why the source of
     truth moved.
  3. `services.EnsureRoles` seeds the three appliance roles **before** the admin is seeded — the
     bootstrap admin has to be given the superadmin role, and the role has to exist to be given.
  4. `localUser.EnsureDefaultAdmin`; on `Seeded`, `announceFirstRunAdmin` prints the bootstrap
     credential to the log — on a fresh install this console banner is the only place a
     CLI/Docker/systemd operator learns it.
  5. Builds the notification service and `services.NewSQLStore(deps.Db)` /
     `services.NewNotificationAlarmer` (`services/alarm.go.md`) — door alarms (duress, tamper,
     forced/held-open, reader offline) land in the same feed an operator already watches.
  6. Builds `cacheClock := services.NewCacheClock(dbsql.NewGenericRepo[sharedentities.RuntimeSetting](deps.Db))`
     (`services/cache_clock.go.md`) — the number `Decide()`'s GATE 10 compares a door's offline TTL
     against, and nothing computed it anywhere outside a unit test until this change; see that file
     for what that meant in production. Then builds and starts the runtime (`newRuntime(deps, live,
     location, store, alarms, alarms.Decision, store.StrikeFor, cacheClock)` / `runtime.start`,
     `app/runtime.go.md`) — one supervised goroutine per configured OSDP bus, now driven by the live
     `services.AccessSettings`/`BusSettings`, not `*pintuconfig.Config`/`BusConfig`. A boot with
     zero configured buses is not an error: the API comes up so a fresh install can be configured
     before any reader is wired. `alarms.Decision` (`services/alarm.go.md`) is the fifth
     argument — every access decision the runtime's controllers make now also reaches the
     notification feed, not just alarms. `runtime.SetOffline(ctx, live.Offline)` is called
     immediately after `start` (not seeded in `newRuntime`) so an appliance whose `config.json`
     already has `access.offline: true` on first boot raises the degraded-mode alert at BOOT — such
     a site never crosses the online→offline edge, and would otherwise run from cache forever with
     nobody told. `settings.OnChange(runtime.ApplySettings)` is then registered — before the API is
     mounted, so no `PUT /api/settings/access` can slip through unapplied — which is what makes
     turning offline mode on from the Settings screen reach the RUNNING controllers; see
     `app/runtime.go.md`'s `ApplySettings`.
  7. Registers `sharedapis.NewLocalLoginApi` on the **public** router (must be mounted before
     the protected subrouter or the auth middleware swallows it), then mounts `protected` with,
     in order, `NewLocalBasicAuth` then `NewRequireRolePermission` — auth before authorization,
     since the matrix needs a principal in context to decide against.
  8. Registers `apis.NewSettingsApi`, `apis.NewDoorApi`, `apis.NewHolderApi`, `apis.NewEventApi`,
     `apis.NewLockdownApi`, `apis.NewSetupApi`, `apis.NewDeploymentApi` (deployment mode / Phase 1
     multi-instance safety — a fixed, read-only `GET /api/deployment/preflight` answering
     `Appliance: true, ApplianceReason: sharedservices.ApplianceSerialBus`: the `osdp.Bus` opens
     its serial port once and holds it for the bus's lifetime, so a second instance cannot share
     it; no `POST /api/deployment/mode` route exists — see `apis/deployment.go.md`),
     `apis.NewNotificationsApi`, `apis.NewAccessRulesApi`
     (`apis/access_rules.go.md` — groups, schedules and grants, the surface that makes a wizard-issued
     badge actually open a door) on `protected`. Immediately before those registrations,
     `protected.Use(ruleChangeTouch(cacheClock))` — a middleware that resets the cache clock after
     every accepted (2xx) `POST`/`PUT`/`PATCH`/`DELETE` under `accessRulePaths` (`/api/doors`,
     `/api/holders`, `/api/groups`, `/api/schedules`, `/api/grants`, `/api/holidays`,
     `/api/settings/access`, `/api/lockdown`). This is the second contact signal
     `services/cache_clock.go.md` describes: an operator who can still administer this appliance IS
     an authority reaching the controller, even with the fleet uplink cut, so an accepted rule edit
     must count exactly as a live control-channel connection does. A badge decision is deliberately
     not in the path list — the whole point of offline mode is that badges keep arriving at a
     controller nobody can reach — and a refused edit (non-2xx, via `statusRecorder`) does not
     touch the clock either.
  9. **Wires the fleet**, gated on `boolValue(deps.Config.Pairing.Enabled, true)`: resolves
     `openFleetSecretCipher(deps)` (fails closed — see `app/wire_fleet.go.md`), builds the fleet
     via `buildFleet(api, deps, appVersion(m), fleetCipher, notifications, cacheClock)`
     (`app/wire_fleet.go.md` — `cacheClock` is what lets a live control-channel connection reset the
     clock too, via `control.SetOnContact`), registers the PUBLIC pairing routes
     (`sharedapis.NewPairingPublicApi`) on the **unauthenticated** router — before the protected
     subrouter, or the auth middleware would swallow the adopt call and the node could never be
     adopted — and the protected pairing routes (`sharedapis.NewPairingApi`) on `protected`, then
     calls `f.start(bgCtx, deps)`. `bgCtx` (a fresh `context.WithCancel(context.Background())`
     created at the top of `RegisterAppRoutes`) bounds the fleet workers; the OSDP runtime keeps its
     own cancel because it predates them and owns its bus supervisors.
  10. The returned shutdown func cancels `bgCtx` then calls `runtime.stop()` — cancels every bus
      supervisor.
- `RegisterWebRoutes(router, deps)` — **new**: serves the SPA shell (`GET /` and `GET /index.html`
  → `<HomeDir>/static/index.html`, `Cache-Control: no-cache, no-store, must-revalidate` on both,
  since `index.html` points at content-hashed chunks and a cached copy would keep a browser on an
  old bundle after an upgrade). Resolved against `deps.HomeDir`, **not** `BaseDir()` — `BaseDir()`
  is the CWD-relative dev path, so a packaged install (binary and `static/` side by side, working
  directory elsewhere) would 404 on `/` if this used it instead; `apphost`'s SPA catch-all uses
  `HomeDir`, and this has to match it. This is the first frontend the app has ever served — see the
  Notes below.
- `settingsFromConfig(cfg)` — converts `config/config.go.md`'s `pintuconfig.Config` into the
  `services.AccessSettings` first-boot seed. Its output is never read again after that first boot;
  it stays as the **reset target** — see `services/runtime_settings.go.md`'s `Reset` — so a settings
  edit that stops the controller from starting can be undone from the UI instead of the database.

## Fleet adoption

`mypintusan` is adopted by `myseliasan` exactly as `mymatasan` and `myiotsan` are, on the same
shared node stack (`domain/shared/fleetnode`). It reports `fleetnode.KindDoor`
(`docs/modules/domain/shared/fleetnode/pairing.go.md`), so the control plane's fleet UI and its
correlator both know a door controller is neither a camera node nor a sensor hub — see
`docs/modules/apps/myseliasan/entities/managed_node.go.md` and
`docs/modules/apps/myseliasan/services/correlate.go.md`.

The event sink `buildFleet` registers (`app/wire_fleet.go.md`) is what makes the fifth app a
fleet citizen: every alarm AND every access **decision** this node raises (`services/alarm.go.md`'s
`NotificationAlarmer.Decision`) also lands in the control plane's unified feed via the
node-dialed control channel, correlatable against camera and sensor events — motion on a camera
AND a door contact opening AND no badge accepted. Neither node can see that alone.

Live-bench verified end-to-end on one machine: UDP discovery (kind hint `"door"` in the unsigned
announce), claim-code adopt (authoritative `KindDoor` in the signed adopt reply), mTLS enrollment
+ cert issuance, the control channel, the embedded management UI over the tunnel, a badge
decision reaching the parent's unified feed tagged `node:<id>`, a fleet rule with door-kind
clauses arming→grace→firing Critical, replay-on-reconnect (5 events missed, zero duplicates), and
a remote unlock issued through the tunnel audited on the node as `"cp:admin"`.

## Notes

- **Shipped so far** (per the file's own header comment): the OSDP driver and simulator
  (`infra/access/osdp`, `tools/osdp-sim`), the decision path, the door state machine, SQLite
  persistence, the app wiring that makes all of it bootable, and — as of this change — a React
  SPA (`views/react-webpack/`, building to `static/`, served by `RegisterWebRoutes`) with a
  first-run wizard, plus database-backed runtime settings replacing `config.json` as the source of
  truth after first boot.
- **What remains**: no reader onboarding beyond the wizard's single reader (no bus discovery, no
  SCBK rekey from the UI); no myiotsan bindings for door contacts/relay actuation — a door contact
  on the **reader's own supervised input** is now read and drives forced/held-open detection, but a
  contact terminating on a myiotsan relay board (`Door.ContactDeviceKey`) still has no path in; and
  no serial bus transport (only `tcp://` dials). `myseliasan` fleet adoption is now wired (see "Fleet adoption" above) — doors as
  placeable floor-plan assets and a `myiotsan` `RelayDeviceKey` binding remain out of scope. The
  groups/schedules/grants screens and APIs that used to be missing here now exist
  (`apis/access_rules.go.md`) — choosing which doors somebody reaches no longer needs direct
  database access, and the first-run wizard grants the person it just created onto the door it
  just created, so a fresh install works end to end without an operator ever visiting that screen.
- `loginGuardConfig(deps)` maps `deps.Config.LoginSecurity.Effective()` onto
  `sharedapis.LoginGuardConfig` — identical shape to the other appliances' own mapping; reading
  through `.Effective()` is what makes an absent `loginSecurity` block resolve to the guard being
  ON by default.
- `announceFirstRunAdmin` is defined in this file rather than a separate `firstrun.go` (unlike
  myidsan/myiotsan/myseliasan/mymatasan) — mypintusan has no other first-run concerns yet (no
  wizard-specific banner text, no capacity estimate) to warrant splitting it out.
- `accessRulePaths`, `ruleChangeTouch(clock)` and `statusRecorder` are new package-level helpers
  in this file, defined between `RegisterAppRoutes` and `RegisterWebRoutes`. `statusRecorder`
  wraps `http.ResponseWriter` to remember the status a handler actually wrote (defaulted to `200`,
  since a handler that never calls `WriteHeader` has written one) so `ruleChangeTouch` can tell an
  accepted edit from a refused one before deciding whether to call `cacheClock.Touch`.
- The first live bench of offline mode (`tools/fleetbench/bench_pintusan_offline.py`, 19/19; 12/19
  against the unfixed app) found that `ControllerConfig.CacheAge` had never been assigned outside a
  unit test, that `createDoorRequest` had no `offlinePolicy`/`offlineTtlSeconds` fields so `deny`
  and a real TTL could not be stored on any door, that `access.offline` was read once at process
  start so a settings-screen edit never reached a running controller, and that nothing raised the
  degraded-mode alert `docs/MYPINTUSAN_DATA_MODEL.md` §2 has always said offline mode should. All
  four are fixed by this file plus `app/runtime.go.md`, `services/cache_clock.go.md`,
  `services/runtime_settings.go.md` and `apis/doors.go.md`. See
  `docs/MYPINTUSAN_OSDP_PLAN.md` §11.
- Live-verified: booted against `tools/osdp-sim` — 23 tables created (the shared appliance
  block plus the 12-table access schema, expanding to individual `CREATE TABLE`/index
  statements), roles and the local admin seeded, the configured bus dialled, 191 granted badge
  events, a strike fired; `GET`/`POST` on `/api/doors`, `/api/readers`, `/api/holders`,
  `/api/events`, `/api/lockdown`, plus login, were all exercised end to end. An operator unlock
  through `POST /api/doors/{id}/unlock` appears in the same access log as a badge, with
  `RawCredential = "operator"`; an unlock attempted during lockdown was refused and logged. See
  `docs/MYPINTUSAN_DATA_MODEL.md` and `docs/MYPINTUSAN_OSDP_PLAN.md` for the wider phase status.
- The SPA and its first-run wizard (`views/react-webpack/src/views/Wizard.js`, gated on
  `GET /api/setup/state` and `user.isAdmin`) were driven live in a real browser, not just unit
  tested, and that surfaced three bugs — none in this file, all in the SPA it now serves: (1) the
  shared `lib/api.js`'s `apiRequest` already `JSON.stringify`s `options.body`, and the app's own
  `send()` helper stringified it a second time, so every write in the SPA failed
  ("cannot unmarshal string into Go value of type ..."); (2) the wizard renders **instead of** the
  app shell, so `ToastStack` was never mounted while it was up and every error inside it was
  invisible — an inline error banner was added, which is what exposed bug (1); (3) the shared
  `DataTable`'s columns are `{key, label, render}` with `render(value, row)`, not `title`/
  `render(row)` — getting that wrong threw during render and unmounted the whole app mid-session.
