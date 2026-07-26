# Module: apps/myseliasan/apis/system.go

## Purpose

Mounts a single system-level operation: a process restart, superadmin-gated, so an operator can
apply a settings change (`apis/settings.go.md`) without a manual restart of the service/process.

## Endpoints

| Method | Path | Notes |
|---|---|---|
| `POST` | `/api/system/restart` | Relaunches the process so any pending config-file change (written by `apis/settings.go`) takes effect. Returns `{"restarting": true}` immediately; the actual relaunch happens ~500ms later. |

## Authorization

- `auth.Middleware` + `session.Middleware` on the `/system` subrouter.
- `requireSuper` (a local copy of the same pattern used in `apis/settings.go`) checks
  `AccessSessionMidware.IsSuperadmin(r)` and returns `ErrLimitedAccess` (403) otherwise —
  restarting the control plane is superadmin-only, same rationale as the settings editor.

## Constructor

`NewSystemApi(router, auth, session, restart)` — `restart` is the local `restarter` interface
(`Restart(reason string)`), satisfied by `apphost.Restarter` (`infra/apphost/types.go`).
Registered in `app.go`'s `RegisterAppRoutes` right after `NewSettingsApi`, passed
`deps.Restarter` directly (no myseliasan-specific restart logic — this just exposes the shared
apphost restart mechanism over HTTP). `restart` may be `nil` if the host did not provide one; in
that case the endpoint reports "restart is not available" (500) rather than panicking.

## `restart` handler

Sends the `{"restarting": true}` JSON response **first**, then relaunches after a
`500 * time.Millisecond` delay in a separate goroutine — the delay lets the HTTP response reach
the client before the process starts tearing down, so the caller can begin polling for the
server to come back (the frontend's `SettingsPage` polls `GET /api/health` for up to 2 minutes,
then reloads the page). The reason string passed to `Restarter.Restart` is
`"settings change: api restart request"`, distinguishing this trigger from other restart paths
(e.g. mymatasan's factory-reset restart) in logs.

## Notes

- Seeded as an `api_endpoint` row in `app.go`'s `Seeders` (`Title: "System"`, `Path:
  /api/system`, `AccessTier: AuthOnly`) for rate-limiting/runtime metadata — the superadmin
  gate itself is enforced in-handler, same as `/api/settings`.
- No audit entry is written for a restart itself; the settings change that necessitated it was
  already audited by `apis/settings.go`'s `record`.
