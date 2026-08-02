# Module: domain/shared/services/setup_state.go

## Purpose

The canonical first-run setup-wizard completion flag, shared across every app in the
suite. Extracted from what used to be near-identical copy-pasted `setup_state.go` files
in `apps/mymatasan/services` and `apps/myidsan/services` (now both deleted), so a fix or
behavior change is made once instead of drifting across apps that copy it next.

## Responsibilities

- `SetupState{Completed, CompletedAt}` — persisted as a single JSON-encoded value under
  the shared `RuntimeSetting` key exported as `SetupStateKey` (`"setup.state"`), so
  completion survives restarts and is shared across browsers/sessions rather than living
  in one browser's `localStorage`.
- `ISetupStateService.Get(ctx)` — reads the row. A missing row, an empty value, or a
  corrupt (unparseable) JSON value are all treated as "not completed" rather than an
  error, so a bad row can never wedge the wizard on a parse failure — the wizard simply
  re-runs.
- `ISetupStateService.Complete(ctx)` — stamps `Completed: true` and
  `CompletedAt: time.Now().UTC().Unix()`, inserting the row if absent or updating it in
  place if present. Calling it again is idempotent: it just refreshes the row.
- `NewSetupStateService(repo)` wraps a `dbsql.IGenericRepo[entities.RuntimeSetting]` —
  the same generic repo every app's `app.go` wires for `sharedentities.RuntimeSetting`.

## Notes

- **The insert guard is deliberately stricter than mymatasan's original copy.**
  `Complete` treats `row == nil || row.Id == 0` as "missing" and inserts, not just a
  `nil` row / not-found error. mymatasan's pre-extraction copy checked only the error
  path; myidsan's copy already had the `row.Id == 0` guard. That guard is now the
  canonical behavior everywhere, because a repo that signals "missing" by returning a
  zero-value row (rather than an error) would otherwise fall through to `UpdateById` on
  id `0` and silently persist nothing — the wizard would then never register as
  completed no matter how many times an operator finished it.
- See `docs/modules/domain/shared/apis/setup.go.md` for the HTTP surface built on top of
  this service.
- Route registration is deliberately **not** shared — see that doc for why each app
  wires `/setup/state` and `/setup/complete` slightly differently.
- Consumers: `apps/mymatasan/app/app.go`, `apps/myidsan/app/app.go`,
  `apps/myseliasan/app/app.go`, `apps/myiotsan/app/app.go` all construct one instance
  per process and pass it to their local `apis.NewSetupApi`. `apps/mymatasan/services/backup.go`
  and `apps/myidsan/services/backup.go` also call `Complete` directly after a successful
  restore, so a rebuilt instance never re-shows the first-run wizard.
