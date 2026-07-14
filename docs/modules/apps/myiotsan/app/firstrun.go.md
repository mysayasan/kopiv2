# Module: apps/myiotsan/app/firstrun.go

## Purpose

First-run bootstrap-admin plumbing for myiotsan: announces (console banner + recovery file)
the admin credential established by `sharedservices.ILocalUserService.EnsureDefaultAdmin`.
Near-identical, app-named copy of mymatasan's own `app/firstrun.go` (and myseliasan's), kept
as a per-app copy rather than shared code because it is glue around each app's own
`Dependencies`/`config.AppConfigModel`, not security-critical logic.

## Responsibilities

- `announceFirstRunAdmin(deps, seed sharedservices.AdminSeedResult)` — called only when
  `seed.Seeded` is true (a fresh install: the user table was empty). Writes
  `INITIAL_ADMIN_LOGIN.txt` (`0o600`) to `deps.DataDir` via `writeFirstRunCredentialFile`,
  then prints a bordered console banner with the console URL, username, and password. The
  password is echoed **only** when `seed.Generated` is true — a config/env-supplied password
  is pointed at (not logged), since the operator already knows it and writing it to a log file
  is a leak. The account is must-change either way. Logs a warning
  (`deps.Logger.Warnf("myiotsan.setup", ...)`) if the recovery file could not be written,
  rather than failing startup.
- `firstRunConsoleURL(cfg)` — picks the URL to show: first configured TLS port
  (`https://localhost:<port>`), else first non-TLS port (`http://localhost:<port>`), else
  `https://localhost:3003` as myiotsan's default.
- `writeFirstRunCredentialFile(path, url, seed)` — writes the recovery file; creates the data
  dir (`0o755`) if needed. Always contains the actual password (unlike the console banner,
  which withholds a non-generated one) because it is local-filesystem-only and is the
  documented recovery path if the console banner is missed (Windows service console, Docker
  log scrollback, etc.).

## Constants

- `firstRunCredentialFile = "INITIAL_ADMIN_LOGIN.txt"`

## Notes

- Unlike myseliasan's `firstrun.go`, myiotsan's P0 has no `RESET_ADMIN` marker / lock-out
  recovery path yet — `EnsureDefaultAdmin` only ever *seeds* (empty user table); there is no
  `ResetAdmin` call site in `app.go` yet. Recovery-marker support can be added the same way
  mymatasan/myseliasan did if myiotsan needs the same self-service unlock flow.
- See `domain/shared/services/local_user_types.go.md` for `AdminSeedResult`, and
  `domain/shared/services/local_user.go.md` for `EnsureDefaultAdmin`'s seeding rules
  (password precedence: `LOCAL_ADMIN_PASSWORD` env → config value → generated).
