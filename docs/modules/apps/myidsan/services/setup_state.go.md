# Module: apps/myidsan/services/setup_state.go

## Purpose

Tracks whether the first-run setup wizard has been completed, mirroring
`apps/mymatasan/services/setup_state.go`'s contract.

## Responsibilities

- `SetupState{Completed, CompletedAt}` — persisted as a single JSON-encoded value
  under the shared `RuntimeSetting` key `"setup.state"`, so completion survives
  restarts and is shared across browsers/sessions (not per-cookie state).
- `ISetupStateService.Get(ctx)` — reads the row. A missing row, an empty value, or a
  corrupt (unparseable) JSON value are all treated as "not completed" rather than an
  error, so a bad row can never wedge the wizard on a parse failure — the wizard
  simply re-runs.
- `ISetupStateService.Complete(ctx)` — stamps `Completed: true` and
  `CompletedAt: time.Now().UTC().Unix()`, creating the row if absent or updating it
  in place if present. Calling it again is idempotent: it just refreshes the row.
- `NewSetupStateService(repo)` wraps a `dbsql.IGenericRepo[entities.RuntimeSetting]`
  — the same generic repo `app/app.go` wires for `sharedentities.RuntimeSetting`.

## Notes

- See `docs/modules/apps/myidsan/apis/setup.go.md` for the HTTP surface.
- See `docs/modules/apps/myidsan/app/app.go.md` for where `RuntimeSetting` is
  registered as a bootstrap entity and the service is constructed.
- The same instance is also handed to `services.NewBackupService`
  (`services/backup.go.md`): a successful restore calls `Complete` so a rebuilt instance
  never re-shows the first-run wizard.
