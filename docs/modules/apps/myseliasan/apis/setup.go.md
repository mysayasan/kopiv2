# Module: apps/myseliasan/apis/setup.go

## Purpose

First-run setup wizard for myseliasan — a capability the app previously had **none** of.
Route registration over the shared `domain/shared/apis.SetupHandlers` (see
`docs/modules/domain/shared/apis/setup.go.md`), mirroring `apps/myidsan/apis/setup.go`.

## Route Group

Base path: `/api/setup`

- `GET /api/setup/state` — auth-only (any signed-in user may read it; the SPA checks it
  right after login to decide whether to show the wizard).
- `POST /api/setup/complete` — auth + `AccessSessionMidware` (the RBAC matrix); the
  wizard only ever runs as a superadmin, who bypasses the matrix, so in practice this is
  a superadmin-only write.

## Handler Behavior

- `NewSetupApi(router, auth, access, setup sharedservices.ISetupStateService)` builds a
  `*sharedapis.SetupHandlers` via `sharedapis.NewSetupHandlers(setup)` and wires its
  exported `State`/`Complete` methods onto the two routes above. No handler logic of its
  own — identical wiring shape to `apps/myidsan/apis/setup.go`.

## Wiring

Registered in `app/app.go` `RegisterAppRoutes` via
`apis.NewSetupApi(api, *deps.Auth, controlSession, sharedservices.NewSetupStateService(runtimeSettingRepo))`,
right after `apis.NewSystemApi`. `sharedentities.RuntimeSetting{}` was added to
`Entities()` to back it — `ControlSetting` (myseliasan's existing key-value table,
holding the fleet key and node-adoption state) is deliberately left alone; the setup
flag rides on the same shared table the rest of the suite uses instead. Seeded as an
endpoint catalog row (`Title: "Setup Wizard"`, `Path: "/api/setup"`, `AccessTier:
AuthOnly`) in `Seeders`.

## Notes

- The SPA's `SetupWizard` (`views/react-webpack/src/views/components/setup.js`, new) is
  the only consumer: `App.js` fetches `/api/setup/state` after the mustchange/pending-
  clearance gates, superadmin-only, and shows the wizard until `completed` is true. A
  failed probe falls through to the normal app rather than locking an operator out of
  their own control plane.
- Six steps: welcome, sign-in (import a myidsan `<code>-sso.json` bundle and save it),
  first site, adopt a node, handover (elevate a real account to superadmin), done.
  Deliberately **no alerts step** (myseliasan has no notification-destination API yet)
  and **no restore-from-backup step** (myseliasan has no backup/restore capability at
  all) — unlike mymatasan's and myidsan's wizards, which both offer a restore path.
- See `docs/modules/domain/shared/services/setup_state.go.md` for the persisted state
  contract (key `setup.state` on the shared `RuntimeSetting` table).
