# Module: apps/myidsan/apis/setup.go

## Purpose

REST API for the first-run setup wizard's completion flag, mirroring
`apps/mymatasan/apis/setup.go`.

## Route Group

Base path: `/api/setup`

- `GET /api/setup/state` — auth-only (any signed-in user may read it; the SPA
  checks it right after login to decide whether to show the wizard).
- `POST /api/setup/complete` — auth + `AccessSessionMidware` (the RBAC matrix); the
  wizard only ever runs as the bootstrap superadmin, who bypasses the matrix, so in
  practice this is a superadmin-only write.

## Handler Behavior

- `state` calls `ISetupStateService.Get` and returns the `SetupState` JSON as-is.
- `complete` calls `ISetupStateService.Complete` and returns the resulting
  `SetupState` (idempotent — calling it again just refreshes `completedAt`).
- Both handlers wrap service errors as `ErrInternalServerError`.

## Wiring

Registered in `app/app.go` `RegisterAppRoutes` via
`apis.NewSetupApi(api, *deps.Auth, deps.Access, services.NewSetupStateService(runtimeSettingRepo))`,
and seeded as an endpoint catalog row (`Path: "/api/setup"`, `AccessTier: DevOnly`) in
`Seeders`.

## Notes

- See `docs/modules/apps/myidsan/services/setup_state.go.md` for the persisted
  state contract.
- The SPA's `SetupWizard` (`views/react-webpack/src/views/components/setup.js`) is
  the only consumer today: `App.js` fetches `/api/setup/state` right after login for
  a superadmin whose forced password change is already cleared, and shows the wizard
  until `completed` is true. Every wizard step posts to APIs that already exist
  (`/api/app-registry`, `/api/app-auth-config`, `/api/app-redirect-uri`,
  `/api/directory-config`, `/api/user-credential`); `/api/setup/complete` only
  records that the operator finished (or skipped) the walkthrough.
