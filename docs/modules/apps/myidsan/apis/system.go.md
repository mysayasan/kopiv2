# Module: apps/myidsan/apis/system.go

## Purpose

Mounts system-level operations under `/api/system`. It exists for the **Settings** editor
(`apis/settings.go.md`): every block that editor can change is read by the shared host only
at boot, so a save reports `needsRestart: true`, and this is the endpoint that applies it.
Without it, the Settings page could tell an operator a restart was required and offer no
way to perform one. Also mounts the **factory reset** surface, built on the shared
`domain/shared/apis/system_reset.go.md` handlers.

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
- `GET /api/system/reset/state` — `{ allowed, confirmPhrase }` from the shared
  `sharedservices.SystemResetService`; the button/dialog stay hidden client-side unless
  `allowed`.
- `POST /api/system/reset` (body `{ "confirm": "<phrase>" }`) — starts a factory reset in
  the background after the shared service re-verifies the typed phrase server-side
  (`ErrConfirmMismatch` on a mismatch) — on the identity provider especially, an endpoint
  that erases every account should not be one stray authenticated request away from firing.
- `GET /api/system/reset/progress` — the in-memory `ResetProgress` of a running/finished
  reset.
- `reset` (the `*sharedservices.SystemResetService`) may be `nil`, in which case none of
  the three reset routes are registered at all.

## Notes

- `restart` is never delegated through the permission matrix — it drops every in-flight
  request on the identity provider the rest of the suite authenticates against, so it is
  superadmin-only unconditionally, the same posture as `/api/backup` and `/api/settings`.
  The reset routes inherit the same superadmin gate and additionally require the typed
  confirmation to match server-side.
- Mounted in `apps/myidsan/app/app.go`'s `RegisterAppRoutes` via
  `apis.NewSystemApi(api, *deps.Auth, deps.Access, deps.Restarter, systemResetService)`,
  right after `apis.NewSettingsApi` — see `apps/myidsan/app/app.go.md`.
- Seeded as an `api_endpoint` row (`Path: "/api/system"`, `AccessTier: AuthOnly`, no menu —
  it is reached only from inside the Settings page's System tab, not a nav item).
- Frontend: the Settings page's System tab (`views/react-webpack/src/views/components/
  settings.js`'s `SystemTab`) calls this after posting, then polls the public `/api/health`
  endpoint until the fresh process answers before reloading the page. The same tab mounts
  the shared `@shared/FactoryResetSection` (Danger Zone) below the restart control, through
  a `resetApi` adapter that re-parses the shared component's stringified JSON body before
  handing it to myidsan's own `apiRequest` — the shared component sends a fetch-style
  string body, but `apiRequest` stringifies whatever it is given, so passing the string
  straight through would double-encode it into a quoted string the server can't decode.
- See `docs/modules/domain/shared/services/system_reset.go.md` and
  `docs/modules/domain/shared/apis/system_reset.go.md` for the reset pipeline and the
  `NewResetGate` middleware that keeps `/system/reset/progress` reachable once the DB pool
  closes.
