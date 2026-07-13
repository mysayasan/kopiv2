# Module: apps/myseliasan/app/firstrun.go

## Purpose

First-run/lock-out-recovery bootstrap-admin plumbing for myseliasan: consumes the
`RESET_ADMIN` recovery marker, and announces (console banner + recovery file) the
stock superadmin credential established by `services.IControlUserService`. Mirrors
mymatasan's own first-run banner so both apps hand an operator a discoverable
credential the same way.

## Responsibilities

- `consumeAdminResetMarker(deps, users)` — the lock-out recovery path. If
  `<dataDir>/RESET_ADMIN` exists, it is **deleted first**, then
  `IControlUserService.ResetStockSuperadmin` is called with `deps.Config.LocalAuth`
  username/password (so a config- or `LOCAL_ADMIN_PASSWORD`-supplied password still
  wins over generating one). Deleting the marker before the reset runs means a crash
  mid-reset (or any later restart) can never silently re-run the reset. Returns
  `(nil, nil)` when no marker is present — the normal boot path. The marker is dropped
  by the Windows installer's "reset the admin login" option, or by hand on any
  platform; it requires local filesystem access to the data dir, so it is not reachable
  over the network.
- `announceFirstRunAdmin(deps, seed)` — called only when `StockSeedResult.Seeded` is
  true (a fresh install, or a reset). Writes `INITIAL_ADMIN_LOGIN.txt` (`0o600`) to the
  data dir via `writeFirstRunCredentialFile`, then prints a bordered console banner
  with the console URL, username, and password. The password is echoed **only** when
  `seed.Generated` is true; a config/env-supplied password is not printed or written
  in full (the operator already knows it — the banner instead points at where it came
  from). The account is must-change either way.
- `firstRunConsoleURL(cfg)` — picks the URL to show: first TLS port (`https://`), else
  first non-TLS port (`http://`), else port `3002` as a last-resort default.
- `writeFirstRunCredentialFile(path, url, seed)` — writes the recovery file; creates
  the data dir (`0o750`) if needed. The file always contains the actual password
  (unlike the console banner, which withholds a non-generated one) because it is
  local-filesystem-only and is the documented recovery path if the console banner is
  missed (Windows service console, Docker log scrollback, etc.).

## Constants

- `firstRunCredentialFile = "INITIAL_ADMIN_LOGIN.txt"`
- `adminResetMarkerFile = "RESET_ADMIN"`

## Call sequence (`app/app.go` `RegisterAppRoutes`)

1. `consumeAdminResetMarker` — if it returns a non-nil `*StockSeedResult`, that is the
   reset outcome and seeding is skipped.
2. Otherwise `EnsureStockSuperadmin` runs normally.
3. If the resulting `seed.Seeded` is true (account created or reset), call
   `announceFirstRunAdmin`.

## Notes

- See `docs/modules/apps/myseliasan/services/rbac.go.md` for `StockSeedResult`,
  `EnsureStockSuperadmin`, and `ResetStockSuperadmin`.
- See `deploy/README-myseliasan.md` for the operator-facing description of first-run
  login and the packaged installer's "reset admin login" option.
