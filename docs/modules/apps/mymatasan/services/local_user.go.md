# Module: apps/mymatasan/services/local_user.go

## Purpose

Implements standalone DB-backed user management for `mymatasan`.

## Responsibilities

- `EnsureDefaultAdmin(ctx, username, password) (AdminSeedResult, error)` seeds the admin account on first startup (no local users exist) using the caller-supplied `localAuth.username`/`localAuth.password` from config; an explicit `LOCAL_ADMIN_PASSWORD` env var overrides the password argument. When no password is supplied (empty config value and no env override — the packaged `deploy/dist/config.json` ships `localAuth.password` empty for exactly this), it **generates a strong random 16-char per-install password** via `generateBootstrapPassword` (crypto/rand, unambiguous charset) rather than seed a shared default. The seeded account is always created with `MustChangePassword=true`. It returns an `AdminSeedResult` (`Seeded`, `Username`, `Password`, `Generated`) so the caller (`app.go`) can reveal the bootstrap login — console banner + `INITIAL_ADMIN_LOGIN.txt` recovery file — on the install paths with no GUI installer finish page. The caller always writes the recovery file; it echoes the password in the console banner only when `Generated` (a config/env value the operator already knows is not logged).
- `ResetAdmin(ctx, username, password) (AdminSeedResult, error)` is the locked-out recovery path (e.g. the Windows installer's "reset admin login" reinstall over an existing data dir). It force-resets the admin account's password to a bootstrap credential (same resolution as seeding via `resolveBootstrapPassword`: `LOCAL_ADMIN_PASSWORD` env → config value → generated), flags it must-change, and returns the credential to reveal. It targets the configured username, else the first admin (`findAdminToReset`); on an empty user table it seeds instead. `app.go` invokes it only after consuming a one-shot installer marker, so it never runs on an ordinary restart.
- On later startups (users already exist), `flagDefaultAdminPassword` force-flags any admin account still on the legacy shipped default (`admin` / `Admin123`) as must-change, so older installs are protected too.
- Hashes local passwords with bcrypt.
- Authenticates Basic Auth credentials and DB-backed auth cookies.
- Lists, creates, updates, resets passwords, and deletes local users.
- Prevents deleting, disabling, or demoting the last active admin user.

## Notes

- This service is intentionally separate from MyIDSan identity and RBAC services.
- `EnsureDefaultAdmin`'s signature takes `username, password` and now returns `(AdminSeedResult, error)`; `app.go` passes `deps.Config.LocalAuth.Username` / `deps.Config.LocalAuth.Password` and, when `Seeded`, calls `announceFirstRunAdmin`.
- The Windows installer generates its own per-install password and injects it via `LOCAL_ADMIN_PASSWORD`, so on Windows the password is env-supplied (`Generated=false`) and the installer's finish page owns the reveal; the app's banner/file path is for CLI/Docker/systemd/portable.
