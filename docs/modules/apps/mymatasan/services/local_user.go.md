# Module: apps/mymatasan/services/local_user.go

## Purpose

Implements standalone DB-backed user management for `mymatasan`.

## Responsibilities

- `EnsureDefaultAdmin(ctx, username, password)` seeds the admin account on first startup (no local users exist) using the caller-supplied `localAuth.username`/`localAuth.password` from config; an explicit `LOCAL_ADMIN_PASSWORD` env var overrides the password argument. Empty username/password fall back to `admin` / `admin` (matching `myseliasan`'s stock superadmin default). The seeded account is always created with `MustChangePassword=true`.
- On later startups (users already exist), `flagDefaultAdminPassword` force-flags any admin account still on the legacy shipped default (`admin` / `Admin123`) as must-change, so older installs are protected too.
- Hashes local passwords with bcrypt.
- Authenticates Basic Auth credentials and DB-backed auth cookies.
- Lists, creates, updates, resets passwords, and deletes local users.
- Prevents deleting, disabling, or demoting the last active admin user.

## Notes

- This service is intentionally separate from MyIDSan identity and RBAC services.
- `EnsureDefaultAdmin`'s signature takes `username, password` (previously no-arg); `app.go` passes `deps.Config.LocalAuth.Username` / `deps.Config.LocalAuth.Password`.
