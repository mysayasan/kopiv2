# Module: apps/myidsan/apis/setup.go

## Purpose

REST API for the first-run setup wizard's completion flag. The handler logic itself now
lives in the shared `domain/shared/apis.SetupHandlers` (see
`docs/modules/domain/shared/apis/setup.go.md`) — this file is myidsan's thin route
registration over it, mirroring `apps/mymatasan/apis/setup.go` and
`apps/myseliasan/apis/setup.go`.

## Route Group

Base path: `/api/setup`

- `GET /api/setup/state` — auth-only (any signed-in user may read it; the SPA
  checks it right after login to decide whether to show the wizard).
- `POST /api/setup/complete` — auth + `AccessSessionMidware` (the RBAC matrix); the
  wizard only ever runs as the bootstrap superadmin, who bypasses the matrix, so in
  practice this is a superadmin-only write.

## Handler Behavior

- `NewSetupApi(router, auth, access, setup sharedservices.ISetupStateService)` builds a
  `*sharedapis.SetupHandlers` via `sharedapis.NewSetupHandlers(setup)` and wires its
  exported `State`/`Complete` methods onto the two routes above. No handler logic lives
  in this package anymore — `state`/`complete` and the internal `setupApi` struct that
  used to hold them were deleted when this file was repointed onto the shared seam.
- `State` calls `ISetupStateService.Get` and returns the `SetupState` JSON as-is.
- `Complete` calls `ISetupStateService.Complete` and returns the resulting
  `SetupState` (idempotent — calling it again just refreshes `completedAt`).
- Both handlers wrap service errors as `ErrInternalServerError`.

## Wiring

Registered in `app/app.go` `RegisterAppRoutes` via
`apis.NewSetupApi(api, *deps.Auth, deps.Access, sharedservices.NewSetupStateService(runtimeSettingRepo))`,
and seeded as an endpoint catalog row (`Path: "/api/setup"`, `AccessTier: DevOnly`) in
`Seeders`.

## Notes

- See `docs/modules/domain/shared/services/setup_state.go.md` for the persisted state
  contract (key `setup.state` on the shared `RuntimeSetting` table) — myidsan's own
  copy of that service (`services/setup_state.go`) was deleted in favor of the shared
  one.
- The SPA's `SetupWizard` (`views/react-webpack/src/views/components/setup.js`) is
  the only consumer today: `App.js` fetches `/api/setup/state` right after login for
  a superadmin whose forced password change is already cleared, and shows the wizard
  until `completed` is true. The wizard is now 6 steps (welcome, app, signin, **scale**,
  admin, done) — the new "scale" step ("Where sessions live") reads
  `GET /api/settings/storage`, offers `POST /api/settings/cache/test`, and saves via
  `PUT /api/settings/storage`; it does not touch this file or the setup-state contract.
  Every other wizard step posts to APIs that already exist (`/api/app-registry`,
  `/api/app-auth-config`, `/api/app-redirect-uri`, `/api/directory-config`,
  `/api/user-credential`); `/api/setup/complete` only records that the operator
  finished (or skipped) the walkthrough.
