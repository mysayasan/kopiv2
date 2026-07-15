# Module: apps/myiotsan/apis/system.go

## Purpose

Registers the one system control the Settings > System tab needed that nothing else already
served: a **restart**. Version (`GET /api/version`) and health (`GET /api/health`, `/api/ready`)
are already served by the shared host runtime — this file exists only to close the remaining
gap, and it exists because of `services.TelemetrySettingsService` (`services/telemetry_settings.go.md`):
its storage/broker knobs are read once at process start, so an edit needs a restart to take
effect, and until this file shipped there was no in-app way to ask for one.

## Responsibilities

- `NewSystemApi(router, r restarter)` mounts `POST /system/restart`.
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

## Notes

- Mounted in `app.go`'s `RegisterAppRoutes` right after `apis.NewSettingsApi`, passing
  `deps.Restarter` — the host-provided restarter, not a myiotsan-local one.
- `services.Policy()` (`services/rbac.go.md`) gained an explicit `/api/system` catalog row,
  admin-only, alongside this file.
- A restart is the only way `/api/settings/telemetry` edits (retention days, write-batcher
  sizing, the embedded broker's listen address) actually take effect — the Telemetry tab links
  directly to this tab's restart button after a save.
