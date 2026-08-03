# Module: apps/myseliasan/apis/system.go

## Purpose

Mounts system-level operations, superadmin-gated: a process restart so an operator can apply a
settings change (`apis/settings.go.md`) without a manual restart of the service/process, plus
myseliasan's own **factory reset** built on the shared
`domain/shared/apis/system_reset.go.md` handlers.

## Endpoints

| Method | Path | Notes |
|---|---|---|
| `POST` | `/api/system/restart` | Relaunches the process so any pending config-file change (written by `apis/settings.go`) takes effect. Returns `{"restarting": true}` immediately; the actual relaunch happens ~500ms later. |
| `GET` | `/api/system/reset/state` | `{ allowed, confirmPhrase }` from the shared `sharedservices.SystemResetService`. |
| `POST` | `/api/system/reset` | Body `{ "confirm": "<phrase>" }`; starts a factory reset in the background after the shared service re-verifies the typed phrase server-side. |
| `GET` | `/api/system/reset/progress` | The in-memory `ResetProgress` of a running/finished reset. |

The three reset routes are registered only when `NewSystemApi` is passed a non-nil
`*sharedservices.SystemResetService`.

## Authorization

- `auth.Middleware` + `session.Middleware` on the `/system` subrouter.
- `requireSuper` (a local copy of the same pattern used in `apis/settings.go`) checks
  `AccessSessionMidware.IsSuperadmin(r)` and returns `ErrLimitedAccess` (403) otherwise —
  restarting the control plane is superadmin-only, same rationale as the settings editor.
  The reset routes go through the same `requireSuper` wrapper, on top of the shared
  service's own server-side confirmation-phrase check on the `POST`.

## Constructor

`NewSystemApi(router, auth, session, restart, reset)` — `restart` is the local `restarter`
interface (`Restart(reason string)`), satisfied by `apphost.Restarter`
(`infra/apphost/types.go`); `reset` is `*sharedservices.SystemResetService` (may be `nil`,
in which case the reset routes are skipped entirely). Registered in `app.go`'s
`RegisterAppRoutes` right after `NewSettingsApi`, passed `deps.Restarter` directly (no
myseliasan-specific restart logic — this just exposes the shared apphost restart mechanism
over HTTP) and the locally-constructed `systemResetService`. `restart` may be `nil` if the
host did not provide one; in that case the restart endpoint reports "restart is not
available" (500) rather than panicking.

## `restart` handler

Sends the `{"restarting": true}` JSON response **first**, then relaunches after a
`500 * time.Millisecond` delay in a separate goroutine — the delay lets the HTTP response reach
the client before the process starts tearing down, so the caller can begin polling for the
server to come back (the frontend's `SettingsPage` polls `GET /api/health` for up to 2 minutes,
then reloads the page). The reason string passed to `Restarter.Restart` is
`"settings change: api restart request"`, distinguishing this trigger from other restart paths
(e.g. mymatasan's factory-reset restart, or this file's own factory-reset restart) in logs.

## Notes

- Seeded as an `api_endpoint` row in `app.go`'s `Seeders` (`Title: "System"`, `Path:
  /api/system`, `AccessTier: AuthOnly`) for rate-limiting/runtime metadata — the superadmin
  gate itself is enforced in-handler, same as `/api/settings`.
- No audit entry is written for a restart itself; the settings change that necessitated it was
  already audited by `apis/settings.go`'s `record`.
- The factory reset is wired in `app.go` right after this constructor: it erases floor plans,
  the cached basemap, and file storage, crypto-erases the fleet secret key, drops and rebuilds
  the database, then restarts. Adopted nodes are **not** notified and must be re-adopted
  afterward. Hidden unless `bootstrap.allowReset` is true, which myseliasan ships **false**.
  See `docs/modules/domain/shared/services/system_reset.go.md`,
  `docs/modules/domain/shared/apis/system_reset.go.md`, and `apps/myseliasan/app/app.go.md`.
