# Module: domain/shared/services/local_user.go

## Purpose

Implements DB-backed appliance user management, shared by every appliance app (`mymatasan`,
`myiotsan`). Moved here from `apps/mymatasan/services/local_user.go` (behavior-preserving:
mymatasan's call sites are aliases in `apps/mymatasan/services/local_user.go` and are
unchanged) so bcrypt handling, session comparison, the auth-verification cache, and the
last-admin guard are fixed in ONE place, not chased across per-app forks.

## Responsibilities

- `EnsureDefaultAdmin(ctx, username, password) (AdminSeedResult, error)` seeds the admin
  account on first startup (no local users exist) using the caller-supplied
  `localAuth.username`/`localAuth.password` from config; an explicit `LOCAL_ADMIN_PASSWORD`
  env var overrides the password argument. When no password is supplied, it **generates a
  strong random 16-char per-install password** via `generateBootstrapPassword` (crypto/rand,
  unambiguous charset) rather than seed a shared default. The seeded account is always
  created with `MustChangePassword=true`. Returns an `AdminSeedResult`
  (`Seeded`/`Username`/`Password`/`Generated`) so the caller's app package can reveal the
  bootstrap login (console banner + recovery file — see `apps/mymatasan/app/firstrun.go` and
  `apps/myiotsan/app/firstrun.go`, which are near-identical, app-named copies).
- `ResetAdmin(ctx, username, password) (AdminSeedResult, error)` is the locked-out recovery
  path. Force-resets the admin account's password to a bootstrap credential (same resolution
  as seeding), flags it must-change, and returns the credential to reveal. Targets the
  configured username, else the first admin (`findAdminToReset`); on an empty user table it
  seeds instead.
- On later startups, `flagDefaultAdminPassword` force-flags any admin account still on the
  legacy shipped default (`admin` / `Admin123`) as must-change, so older installs are
  protected too.
- Hashes local passwords with bcrypt.
- Authenticates Basic Auth credentials (`Authenticate`) and DB-backed session-cookie hashes
  (`AuthenticateSession`).
- Lists, creates, updates, resets passwords, and deletes local users.
- Prevents deleting, disabling, or demoting the last active admin user
  (`ensureNotRemovingLastAdmin`), counted by **role** (`RoleId == adminRoleId`), not the
  legacy `IsAdmin` bool.
- `BackfillRoles(ctx, adminRoleId, defaultRoleId) (int, error)` gives a role to every user
  with `RoleId == 0`, derived from their legacy `IsAdmin` bool. Runs once at startup, after
  the app's roles are seeded.
- `Authenticate` caches a **successful** Basic Auth verification for `authCacheTTL` (30s),
  keyed by `username + sha256(password)`, so a client replaying the same Basic credential on
  every request skips both the bcrypt compare and the `LastLoginAt` DB write on a cache hit —
  the two per-request costs that otherwise cap throughput under load. Only successes are
  cached (a wrong password always pays bcrypt, so the cache can't cheapen credential
  guessing), and the cache is bounded by the real user count. Flushed on any user mutation
  (`Update`, `ResetPassword`, `ChangePassword`, `Delete`, `ResetAdmin`) so a rotated,
  deactivated, or deleted credential can never keep authenticating from a stale entry.

## Notes

- `roles IAccessRoleService` on `localUserService` is what makes `AuthenticatedUser.IsAdmin`
  a **derived** value (the resolved role's `IsSuperadmin` flag) rather than a second,
  independent source of truth alongside `RoleId`. A role that cannot be resolved is an
  **error** in `identity()`, not a quiet downgrade to non-admin — silently demoting the only
  administrator because a lookup failed would lock the operator out with no way to tell why.
- `resolveRole(ctx, roleId, legacyIsAdmin)`: a positive `RoleId` in a create/update request is
  the authority; `0` falls back to the legacy `IsAdmin` bool (`true` → superadmin, otherwise
  viewer), so a client that predates roles keeps working.
- This service is intentionally separate from MyIDSan identity and RBAC services — it is a
  standalone, per-appliance local user store, not federated identity.
- The auth cache (`authCache`/`authMu`) is in-process and per-app-instance: `mymatasan` and
  `myiotsan` each construct their own `localUserService` (via `NewLocalUserService`) and so
  each hold their own cache — the extraction shares the *implementation*, not a running
  instance or its state.
