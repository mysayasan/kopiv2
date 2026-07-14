# Module: apps/mymatasan/entities/local_user.go

## Purpose

Defines the standalone local login user persisted by `mymatasan`.

## Notes

- `Username` is unique and used for HTTP Basic Auth login.
- `PasswordHash` is omitted from JSON responses.
- `RoleId int64` is the **authority** on what this user may do — it points at a shared
  `AccessRole` row, and that role's permission matrix (`domain/shared/services/access_rbac.go`)
  decides every request via `apis.NewRequireRolePermission`. It replaces `IsAdmin` as a
  single bool that could only express "admin or not"; the three built-in roles
  (`superadmin`/`operator`/`viewer`) are described in `apps/mymatasan/services/rbac.go`.
- `IsAdmin` is now a **legacy mirror**, not the authority: it is written from the resolved
  role (`isSuperadmin`) on every create/update, and it is what
  `ILocalUserService.BackfillRoles` reads to assign a role to a pre-roles row on first boot
  (`true` → superadmin, otherwise operator). It is never read to make an authorization
  decision — `AuthenticatedUser.IsAdmin` is derived from `RoleId` instead
  (`services.identity()`). Kept only so the Users screen's admin badge and API clients that
  predate roles keep working; can be dropped in a migration once every install has been
  backfilled.
- `IsActive` disables login without deleting the row.
- `LastLoginAt`, `CreatedAt`, and `UpdatedAt` support simple local audit metadata.
