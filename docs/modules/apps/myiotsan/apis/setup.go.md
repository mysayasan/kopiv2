# Module: apps/myiotsan/apis/setup.go

## Purpose

First-run wizard completion flag, promoted off a `localStorage` key
(`myiotsan_wizard_done`) and onto the shared server-side `setup.state` seam (see
`docs/modules/domain/shared/apis/setup.go.md`) — the same contract mymatasan, myidsan
and myseliasan use. Dismissal used to be per-**browser**: the same admin signing in from
a second machine, or after clearing site data, met the wizard again on a hub that had
been running for months. It is a property of the install, so it now lives on the server.

## Route Group

Base path: `/api/setup`

- `GET /api/setup/state` — open at the router level (any signed-in user may read it; the
  SPA checks it right after login).
- `POST /api/setup/complete` — open at the router level, but wrapped locally by
  `setupApi.complete`, which requires `sharedapis.LocalUserFromContext(...).IsAdmin`
  before delegating to the shared handler. myiotsan has no RBAC-matrix middleware on
  this subrouter, unlike myidsan/myseliasan, so the admin check is enforced in-handler
  instead — matching the settings surface's gate: the wizard is only ever shown to an
  admin, so a non-admin dismissing it for the whole install would be dismissing
  something they were never offered.

## Handler Behavior

- `setupApi{handlers *sharedapis.SetupHandlers}` wraps `sharedapis.NewSetupHandlers(setup)`.
- `GET /setup/state` is mounted directly on `h.handlers.State`.
- `POST /setup/complete` is mounted on the local `complete` wrapper (admin check, then
  `a.handlers.Complete(w, r)`).

## Wiring

Registered in `app/app.go` `RegisterAppRoutes` via
`apis.NewSetupApi(protected, sharedservices.NewSetupStateService(dbsql.NewGenericRepo[sharedentities.RuntimeSetting](deps.Db)))`,
right after `apis.NewSystemApi`. Seeded as an endpoint catalog row (`Title: "Setup
Wizard"`, `Path: "/api/setup"`, `AccessTier: AuthOnly`) in `Seeders`.

## Notes

- `views/react-webpack/src/views/App.js`'s `wizardDismissed` state now initializes to
  `true` and is set from `GET /api/setup/state`'s `completed` field (fetched alongside
  the existing `GET /api/devices?limit=1` estate-emptiness probe) instead of reading
  `localStorage`. A failed probe leaves the wizard dismissed, so an unreachable endpoint
  cannot start throwing a first-run dialog at an established hub. `dismissWizard` now
  calls `POST /api/setup/complete` instead of writing to `localStorage`.
- The wizard itself (`views/react-webpack/src/views/components/onboarding.js`'s
  `FirstRunWizard`) still only opens when the device estate is genuinely empty
  (`GET /api/devices?limit=1`) — the setup-state flag only stops it reappearing for an
  admin who already dismissed it, it does not gate whether it can show in the first
  place. Its last two steps (`enrol`, `ready`) now also read live hub state
  (`GET /api/discovery/window`, `GET /api/discovery/candidates`) so they report whether
  an enrollment window is open right now and how many candidates are already waiting in
  quarantine, rather than describing the mechanism in the abstract.
- See `docs/modules/domain/shared/services/setup_state.go.md` for the persisted state
  contract.
