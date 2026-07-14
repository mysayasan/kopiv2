# Module: domain/shared/services/local_user_types.go

## Purpose

The appliance user model's public types — the login/identity contract shared by every
appliance app (`mymatasan`, `myiotsan`). Pulled out of `apps/mymatasan/services/ifaces.go`
(where `AuthenticatedUser`, `ILocalUserService`, and the request DTOs used to live) alongside
the implementation move (`local_user.go.md`), for the same reason: this is security-critical
code, and a fix has to land once, not be chased across forks.

## Key Types

- `AuthenticatedUser` — the principal attached to an authenticated request. `RoleId` is the
  authority every request is decided against. `IsAdmin` is **derived** from the resolved
  role's `IsSuperadmin` flag — never read from the legacy `LocalUser.IsAdmin` column — so
  handlers that ask "is this an admin?" (settings self-gates, the control dispatcher) can
  never disagree with the permission matrix. `SessionHash` is `json:"-"` (never serialized).
- `AdminSeedResult` — reports what `EnsureDefaultAdmin`/`ResetAdmin` did on startup, so the
  caller can surface the bootstrap credentials on a fresh install (console banner + recovery
  file, for CLI/Docker/package/portable runs with no GUI installer finish page). `Generated`
  is true only when the password was randomly generated (neither config nor
  `LOCAL_ADMIN_PASSWORD` supplied one) — i.e. the operator does not know it yet and it must be
  revealed.
- `CreateLocalUserRequest` / `UpdateLocalUserRequest` — `RoleId` is the authority when > 0;
  `0` falls back to the legacy `IsAdmin` bool (`true` → superadmin, otherwise viewer), so a
  client that predates roles keeps working.
- `ChangeLocalUserPasswordRequest` — the self-service password-change body
  (`CurrentPassword`/`NewPassword`).
- `ResetLocalUserPasswordRequest` — an admin-driven password reset body.
- `ILocalUserService` — the appliance user store contract: `EnsureDefaultAdmin`,
  `ResetAdmin`, `Authenticate`, `AuthenticateSession`, `Get`, `Create`, `Update`,
  `ResetPassword`, `ChangePassword`, `Delete`, `BackfillRoles`. Implemented by
  `local_user.go`'s `localUserService`.

## Notes

- `apps/mymatasan/services/local_user.go` re-exports every type here as a same-named alias
  (`type AuthenticatedUser = sharedservices.AuthenticatedUser`, etc.) so mymatasan's existing
  call sites — handlers, other services, tests — keep compiling unchanged.
- `apps/mymatasan/services/ifaces.go` no longer declares any of these types; see
  `apps/mymatasan/services/ifaces.go.md`.
