# Module: domain/shared/apis/setup.go

## Purpose

Shared HTTP handler pair for the first-run setup wizard's completion flag, built on top
of `sharedservices.ISetupStateService` (`domain/shared/services/setup_state.go.md`).

## Behavior

- `SetupHandlers{setup}` / `NewSetupHandlers(setup)` — constructs the handler pair.
- `State(w, r)` — `GET` handler. Calls `setup.Get` and returns the `SetupState` JSON
  as-is. Intended to be reachable by any signed-in user, since the SPA checks it
  immediately after login to decide whether to show the wizard.
- `Complete(w, r)` — `POST` handler. Calls `setup.Complete` and returns the resulting
  `SetupState`. This is a write, so callers must register it behind whatever admin gate
  the app uses.

## Notes

- **Route registration is deliberately left to each app**, not shared, because the four
  apps gate the two routes differently:
  - `apps/mymatasan/apis/setup.go` — `GET /setup/state` open, `POST /setup/complete`
    behind mymatasan's global `NewRequireAdminForWrites` wrapper.
  - `apps/myidsan/apis/setup.go`, `apps/myseliasan/apis/setup.go` — `GET /setup/state`
    behind `auth.Middleware` only, `POST /setup/complete` behind
    `auth.Middleware` + `AccessSessionMidware` (the RBAC matrix); the wizard only ever
    runs as the bootstrap superadmin, who bypasses the matrix, so this is a
    superadmin-only write in practice.
  - `apps/myiotsan/apis/setup.go` — both routes open at the router level, but
    `complete` is wrapped locally to require `sharedapis.LocalUserFromContext(...).IsAdmin`
    on top of the shared handler, since myiotsan has no RBAC-matrix middleware on this
    subrouter.
- This file replaces four near-identical `state`/`complete` handler implementations that
  used to live one per app (three of them copy-pasted, one — myiotsan's — driven by a
  `localStorage` key instead of a server call before this change). Each app's
  `apis/setup.go` is now a thin route-registration wrapper over this shared pair.
