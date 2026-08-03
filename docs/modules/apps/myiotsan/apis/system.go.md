# Module: apps/myiotsan/apis/system.go

## Purpose

Registers the system controls the Settings > System tab needs: a **restart**, and myiotsan's
own **factory reset**. Version (`GET /api/version`) and health (`GET /api/health`, `/api/ready`)
are already served by the shared host runtime — the restart half exists only to close that
remaining gap, and it exists because of `services.TelemetrySettingsService`
(`services/telemetry_settings.go.md`): its storage/broker knobs are read once at process start,
so an edit needs a restart to take effect, and until this file shipped there was no in-app way
to ask for one. The reset half is built on the shared
`domain/shared/apis/system_reset.go.md` handlers — myiotsan previously had **no factory reset
at all** (unlike mymatasan's Secure Wipe & Reset).

## Responsibilities

- `NewSystemApi(router, r restarter, reset *sharedservices.SystemResetService)` mounts:
  - `POST /system/restart`.
  - `GET /system/reset/state`, `POST /system/reset`, `GET /system/reset/progress` — only
    when `reset` is non-nil.
  - `restarter` is the minimal slice this file needs (`Restart(reason string)`), satisfied by
    `apphost.Restarter` — the same self-restart mechanism the Secure Wipe & Reset feature and
    `mymatasan` already use (see `restart-relaunch-mechanism` conventions elsewhere in the
    suite).
- `restart` handler: gated by `requireAdminUser` (defence in depth on top of the RBAC matrix),
  responds `200 {"restarting": true}` **before** actually restarting — a `go func` sleeps 500ms
  then calls `a.restarter.Restart("api restart request")`, so the caller's browser gets its
  response and can show a "restarting…" overlay before the process actually goes down. If no
  restarter was wired (`a.restarter == nil`), responds `500` rather than silently doing nothing.
- `requireAdminUser` reads `sharedapis.LocalUserFromContext` and requires `user.IsAdmin`; this is
  the same defence-in-depth pattern `apis/settings.go`'s `requireAdmin` uses, duplicated here
  rather than shared because the two files sit in different structs.
- `requireAdmin` — a second, near-identical admin gate added for the three reset routes,
  wrapping the shared `sharedapis.SystemResetHandlers`. It is a separate function
  (`requireAdmin`, not `requireAdminUser`) because it wraps `http.HandlerFunc` rather than
  returning a `bool`, matching the pattern `apis/settings.go`'s `requireAdmin` already uses;
  functionally it enforces the same `user.IsAdmin` check as `requireAdminUser` on top of the
  RBAC matrix — defence in depth on the one route that can erase the whole hub.

## Notes

- Mounted in `app.go`'s `RegisterAppRoutes` right after `apis.NewSettingsApi`, passing
  `deps.Restarter` — the host-provided restarter, not a myiotsan-local one — and the
  locally-built `systemResetService`.
- `services.Policy()` (`services/rbac.go.md`) gained an explicit `/api/system` catalog row,
  admin-only, alongside this file.
- A restart is the only way `/api/settings/telemetry` edits (retention days, write-batcher
  sizing, the embedded broker's listen address) actually take effect — the Telemetry tab links
  directly to this tab's restart button after a save.
- The factory reset erases file storage and the entire at-rest key **directory** (not just the
  key file, via `KeyStore.Destroy` like the other two apps) — see `app/app.go.md` for why.
  Enrolled devices are **not** notified: each keeps its provisioned broker password and
  reconnects to a hub that no longer knows it, landing back in quarantine as a candidate. If
  this hub is itself adopted by a control plane, the reset also drops its fleet enrollment.
  Unlike myseliasan/myidsan, myiotsan ships `bootstrap.allowReset: true` by default, so the
  Danger Zone panel is visible out of the box. See
  `docs/modules/domain/shared/services/system_reset.go.md` and
  `docs/modules/domain/shared/apis/system_reset.go.md`.
