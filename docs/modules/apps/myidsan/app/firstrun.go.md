# Module: apps/myidsan/app/firstrun.go

## Purpose

First-run/lock-out-recovery bootstrap-admin plumbing for myidsan: consumes the
`RESET_ADMIN` recovery marker, consumes the `RESET_MFA` recovery marker, and
announces (console banner + recovery file) the stock superadmin credential
established by `services.IUserLoginService`. Ported from
`apps/myseliasan/app/firstrun.go` so both apps hand an operator a discoverable
credential the same way.

## Responsibilities

- `consumeMfaResetMarker(deps, mfaService, users)` — the second-factor lock-out
  recovery path: the documented escape hatch for "the sole superadmin lost the
  authenticator device **and** the recovery codes". If `<dataDir>/RESET_MFA`
  exists, it is **deleted first** (so a crash mid-reset, or any later restart,
  can never silently re-clear MFA), then the stock superadmin is looked up by
  `deps.Config.LocalAuth.Username` and its second factor is cleared via
  `mfaService.Disable`. Resets **only** the second factor, never the password —
  pair it with `RESET_ADMIN` if both are needed. A missing stock-superadmin
  account (marker dropped on an install where it was renamed/removed) logs a
  `WARNING` and is a no-op, not a boot failure. No-op (returns `nil`
  immediately) when the marker is absent — the normal boot path.
- `consumeAdminResetMarker(deps, users, superRoleId)` — the lock-out recovery path.
  If `<dataDir>/RESET_ADMIN` exists, it is **deleted first**, then
  `IUserLoginService.ResetStockSuperadmin` is called with `deps.Config.LocalAuth`
  username/password (so a config- or `LOCAL_ADMIN_PASSWORD`-supplied password still
  wins over generating one). Deleting the marker before the reset runs means a crash
  mid-reset (or any later restart) can never silently re-run the reset. Returns
  `(nil, nil)` when no marker is present — the normal boot path. The marker is
  dropped by an installer's "reset the admin login" option, or by hand on any
  platform; it requires local filesystem access to the data dir, so it is not
  reachable over the network.
- `announceFirstRunAdmin(deps, seed)` — called only when `StockSeedResult.Seeded` is
  true (a fresh install, or a reset). Writes `INITIAL_ADMIN_LOGIN.txt` (`0o600`) to
  the data dir via `writeFirstRunCredentialFile`, then prints a bordered console
  banner with the console URL, username, and password. The password is echoed
  **only** when `seed.Generated` is true; a config/env-supplied password is not
  printed or written in full (the operator already knows it — the banner instead
  points at where it came from). The account is must-change either way.
- `firstRunConsoleURL(cfg)` — picks the URL to show: first TLS port (`https://`),
  else first non-TLS port (`http://`), else port `3001` as a last-resort default.
- `writeFirstRunCredentialFile(path, url, seed)` — writes the recovery file; creates
  the data dir (`0o750`) if needed. The file always contains the actual password
  (unlike the console banner, which withholds a non-generated one) because it is
  local-filesystem-only and is the documented recovery path if the console banner is
  missed (Windows service console, Docker log scrollback, etc.).

## Constants

- `firstRunCredentialFile = "INITIAL_ADMIN_LOGIN.txt"`
- `adminResetMarkerFile = "RESET_ADMIN"`
- `mfaResetMarkerFile = "RESET_MFA"`

## Call sequence (`app/app.go` `RegisterAppRoutes`)

1. `consumeAdminResetMarker` — if it returns a non-nil `*StockSeedResult`, that is
   the reset outcome and seeding is skipped.
2. Otherwise `EnsureStockSuperadmin` runs normally.
3. If the resulting `seed.Seeded` is true (account created or reset), call
   `announceFirstRunAdmin`.
4. Separately, before the login/MFA APIs are constructed, `consumeMfaResetMarker`
   runs once `mfaService` exists — independent of the admin-reset sequence above,
   since a password reset and an MFA reset are two different lockouts an operator
   may need one, the other, or both.

## Notes

- See `docs/modules/apps/myidsan/services/user_login.go.md` for `StockSeedResult`,
  `EnsureStockSuperadmin`, and `ResetStockSuperadmin`.
- See `docs/modules/apps/myidsan/services/mfa.go.md` for `IMfaService.Disable`.
- See `apps/myidsan/README.md`'s first-run section for the operator-facing
  description of first-run login and the `RESET_ADMIN`/`RESET_MFA` recovery
  markers.
