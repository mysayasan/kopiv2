# Module: apps/myseliasan/apis/settings.go

## Purpose

HTTP surface for the in-app editor over the safe subset of `config.json`
(`services.ISettingsService`, `services/settings.go.md`).

## Endpoints

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/settings` | Every editable section (secrets masked) plus `sections` (the display-order id list). |
| `POST` | `/api/settings/cache/test` | Live Redis connectivity test (`services.ISettingsService.TestCache`); body capped at 256 KiB. A blank `password` (and `address`) falls back to the stored value, so an existing config — or one still being edited, unsaved — can be tested. Returns `{"ok": true}` on success; a failed ping is a 400 with the connection error as the message. Never writes anything. |
| `GET` | `/api/settings/fs/browse` | Whitelisted, read-only server-side file/folder picker (`services.BrowseDirectory`) behind the storage/logging/TLS/SSO path fields — `?path=` (absent/empty lists the allowed roots). |
| `GET` | `/api/settings/{section}` | One section's values, secrets masked. Unknown `{section}` → 400. |
| `PUT` | `/api/settings/{section}` | Validate + persist a section; body capped at 256 KiB (`maxSettingsBody`). Response is `SaveResult{needsRestart}`. |
| `POST` | `/api/settings/{section}/reset` | Restore a section to its captured first-run defaults. Response is `SaveResult{needsRestart}`. |

The `cache/test` and `fs/browse` literal routes are registered on the `/settings` subrouter
**before** the `/{section}` var routes so gorilla/mux's `{section}` pattern never captures them.

## Authorization

- `auth.Middleware` + `session.Middleware` on the `/settings` subrouter — same as every other
  control-plane API.
- Every handler is additionally wrapped in `requireSuper`, which checks
  `AccessSessionMidware.IsSuperadmin(r)` and returns `ErrLimitedAccess` (403) otherwise. This
  is enforced in-handler, not delegated to the accessrbac permission matrix: these values
  (TLS, rate limit, pairing ports, SSO) can take the whole control plane offline or open a
  security hole, so they are never granted to a lesser role regardless of what the matrix says.

## Constructor

`NewSettingsApi(router, auth, session, settings, audit, browseRoots)` — mounts the six routes on a
`/settings` subrouter. `settings` is `services.ISettingsService`; `audit` is
`services.IAuditService`; `browseRoots` are extra directories the file picker may browse on top of
its built-in whitelist. Registered in `app.go`'s `RegisterAppRoutes` right after `NewReportsApi`,
with `settingsService` built from `deps.Config`, `deps.ConfigPath`, `deps.Db`, the fleet
`secretCipher`, and a `logf` closure routed through `deps.Logger.Warnf`; `browseRoots` is
`[]string{deps.DataDir, deps.HomeDir}`.

## Audit trail

`saveSection`/`resetSection` each call `record`, which writes a best-effort audit entry
(`settings.save` / `settings.reset`, `TargetType: "settings"`, `TargetId: <section>`) via
`services.IAuditService.Record` — **never the values themselves**, which may include secrets;
only which section changed and by whom (`operatorIdentity`/`operatorUserId` from
`apis/node_proxy.go.md` / `apis/node_access_api.go.md`, `clientIP` from `apis/audit.go.md`).
`getSection`/`getAll`/`testCache`/`browseFilesystem` are reads (or, for `testCache`, a
non-persisting connectivity check) and are not audited.

## Notes

- Seeded as an `api_endpoint` row in `app.go`'s `Seeders` (`Title: "Settings"`, `Path:
  /api/settings`, `AccessTier: AuthOnly`) for rate-limiting/runtime metadata — the superadmin
  gate itself is enforced in-handler, the same pattern the security PDF report uses
  (`apis/reports.go.md`). `cache/test` and `fs/browse` are covered by the same `/api/settings`
  endpoint row (no separate seed) since they are sub-paths of it.
- Applying a saved/reset change requires a restart — see `apis/system.go.md`'s
  `POST /api/system/restart`; the frontend's `SettingsPage` surfaces `needsRestart` as a
  persistent banner with a one-click restart action.
- `browseFilesystem` and `testCache` are both purely diagnostic/read-only from the API's
  perspective — neither can be used to write a file, read file contents, or change the live
  config; see `services/filesystem_browse.go.md` and `services/settings.go.md`'s `TestCache`.
