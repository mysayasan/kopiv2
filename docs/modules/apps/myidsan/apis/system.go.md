# Module: apps/myidsan/apis/system.go

## Purpose

Mounts system-level operations under `/api/system`. It exists for the **Settings** editor
(`apis/settings.go.md`): every block that editor can change is read by the shared host only
at boot, so a save reports `needsRestart: true`, and this is the endpoint that applies it.
Without it, the Settings page could tell an operator a restart was required and offer no
way to perform one.

## Routes

Superadmin-gated as **middleware** on the whole route group — matching every other
sensitive myidsan surface (settings, audit, backup, mfa-admin) — rather than a per-handler
wrapper:

- `POST /api/system/restart` (`restart`) — relaunches the process
  (`restarter.Restart("settings change: api restart request")`, satisfied by
  `apphost.Restarter`). The HTTP response (`{restarting: true}`) is sent **before** the
  relaunch, on a 500ms delay in a goroutine, so the client can begin polling for the server
  to come back — a restart that tore down the listener first would look like the request
  itself had failed.
  - `restart` may be `nil` when the host provided no restarter (`NewSystemApi`'s `restart`
    parameter), in which case the endpoint reports `ErrInternalServerError` ("restart is not
    available") rather than silently doing nothing.

## Notes

- `restart` is never delegated through the permission matrix — it drops every in-flight
  request on the identity provider the rest of the suite authenticates against, so it is
  superadmin-only unconditionally, the same posture as `/api/backup` and `/api/settings`.
- Mounted in `apps/myidsan/app/app.go`'s `RegisterAppRoutes` via
  `apis.NewSystemApi(api, *deps.Auth, deps.Access, deps.Restarter)`, right after
  `apis.NewSettingsApi` — see `apps/myidsan/app/app.go.md`.
- Seeded as an `api_endpoint` row (`Path: "/api/system"`, `AccessTier: AuthOnly`, no menu —
  it is reached only from inside the Settings page's System tab, not a nav item).
- Frontend: the Settings page's System tab (`views/react-webpack/src/views/components/
  settings.js`'s `SystemTab`) calls this after posting, then polls the public `/api/health`
  endpoint until the fresh process answers before reloading the page.
