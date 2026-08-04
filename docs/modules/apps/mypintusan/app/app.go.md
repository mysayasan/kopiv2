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
  `/api/events`, `/api/lockdown`, `/api/settings`, `/api/setup`, plus `/api/health`,
  `/api/version`, `/api/auth/login`, `/api/auth` endpoint catalog rows, the same
  insert-if-absent / repair-app_code-and-tier SQL pattern the other appliances use.
- `RegisterAppRoutes(api, deps)`:
  1. Resolves the site timezone **first**, via `cfg.Location()`, and refuses to start if it is
     wrong (`config/config.go.md`) — schedules and holidays are local concepts, and a silent
     fall back to UTC would shift every schedule on the site.
  2. `services.EnsureRoles` seeds the three appliance roles **before** the admin is seeded — the
     bootstrap admin has to be given the superadmin role, and the role has to exist to be given.
  3. `localUser.EnsureDefaultAdmin`; on `Seeded`, `announceFirstRunAdmin` prints the bootstrap
     credential to the log — on a fresh install this console banner is the only place a
     CLI/Docker/systemd operator learns it.
  4. Builds the notification service and `services.NewSQLStore(deps.Db)` /
     `services.NewNotificationAlarmer` (`services/alarm.go.md`) — door alarms (duress, tamper,
     forced/held-open, reader offline) land in the same feed an operator already watches.
  5. Builds and starts the runtime (`newRuntime` / `runtime.start`, `app/runtime.go.md`) — one
     supervised goroutine per configured OSDP bus. A boot with zero configured buses is not an
     error: the API comes up so a fresh install can be configured before any reader is wired.
  6. Registers `sharedapis.NewLocalLoginApi` on the **public** router (must be mounted before
     the protected subrouter or the auth middleware swallows it), then mounts `protected` with,
     in order, `NewLocalBasicAuth` then `NewRequireRolePermission` — auth before authorization,
     since the matrix needs a principal in context to decide against.
  7. Registers `apis.NewDoorApi`, `apis.NewHolderApi`, `apis.NewEventApi`,
     `apis.NewLockdownApi`, `apis.NewSetupApi` on `protected`.
  8. The returned shutdown func calls `runtime.stop()` — cancels every bus supervisor.

## Notes

- **Shipped so far** (per the file's own header comment): the OSDP driver and simulator
  (`infra/access/osdp`, `tools/osdp-sim`), the decision path, the door state machine, SQLite
  persistence, and — as of this file — the app wiring that makes all of it bootable.
- **What remains**: no frontend (this app serves an API only — no `views/`, no `static/`, no
  SPA, no firstrun wizard UI), no myiotsan bindings for door contacts/relay actuation
  (`Controller.ContactChanged` is a seam nothing calls), and no `myseliasan` fleet adoption
  (needs a `fleetnode` node kind that does not exist yet — `domain/shared/fleetnode` declares
  only `KindCamera` and `KindIot`).
- `loginGuardConfig(deps)` maps `deps.Config.LoginSecurity.Effective()` onto
  `sharedapis.LoginGuardConfig` — identical shape to the other appliances' own mapping; reading
  through `.Effective()` is what makes an absent `loginSecurity` block resolve to the guard being
  ON by default.
- `announceFirstRunAdmin` is defined in this file rather than a separate `firstrun.go` (unlike
  myidsan/myiotsan/myseliasan/mymatasan) — mypintusan has no other first-run concerns yet (no
  wizard-specific banner text, no capacity estimate) to warrant splitting it out.
- Live-verified: booted against `tools/osdp-sim` — 23 tables created (the shared appliance
  block plus the 12-table access schema, expanding to individual `CREATE TABLE`/index
  statements), roles and the local admin seeded, the configured bus dialled, 191 granted badge
  events, a strike fired; `GET`/`POST` on `/api/doors`, `/api/readers`, `/api/holders`,
  `/api/events`, `/api/lockdown`, plus login, were all exercised end to end. An operator unlock
  through `POST /api/doors/{id}/unlock` appears in the same access log as a badge, with
  `RawCredential = "operator"`; an unlock attempted during lockdown was refused and logged. See
  `docs/MYPINTUSAN_DATA_MODEL.md` and `docs/MYPINTUSAN_OSDP_PLAN.md` for the wider phase status.
