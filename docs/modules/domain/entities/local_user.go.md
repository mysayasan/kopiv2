# Module: domain/entities/local_user.go

## Purpose

The appliance user model, shared by every appliance app (`mymatasan`, `myiotsan`). Moved
here from `apps/mymatasan/entities/local_user.go` so a second appliance app runs the same
DB-backed login row instead of forking it.

## Notes

- `Username` is unique and used for HTTP Basic Auth login and the session-cookie login
  endpoint.
- `PasswordHash` is omitted from JSON responses.
- `RoleId int64` is the **authority** on what this user may do — it points at a shared
  `AccessRole` row, and that role's permission matrix (`domain/shared/services/access_rbac.go`)
  decides every request. Each app supplies its own catalog (`apps/mymatasan/services/rbac.go`,
  `apps/myiotsan/services/rbac.go`); the mechanics that turn a catalog into permission rows
  are shared (`domain/shared/services/appliance_rbac.go`).
- `IsAdmin` is a **legacy mirror**, not the authority: it is written from the resolved role
  (`isSuperadmin`) on every create/update, and it is what `ILocalUserService.BackfillRoles`
  reads to assign a role to a pre-roles row on first boot (`true` → superadmin, otherwise
  operator). It is never read to make an authorization decision —
  `AuthenticatedUser.IsAdmin` is derived from `RoleId` instead. Kept only so a Users screen's
  admin badge and API clients that predate roles keep working; can be dropped in a migration
  once every install has been backfilled.
- `IsActive` disables login without deleting the row.
- `LastLoginAt`, `CreatedAt`, and `UpdatedAt` support simple local audit metadata.

## Load-bearing constraint: the struct name

The code-first bootstrap derives the table name by reflecting the struct name
(`strcase.ToSnake(typeOf.Name())`). That means this struct **must stay named `LocalUser`** —
renaming it (or aliasing it to something else) would rename the `local_user` table out from
under every deployed appliance on its next boot. `apps/mymatasan/entities/local_user.go`'s
`type LocalUser = entities.LocalUser` alias exists to keep mymatasan's call sites compiling
unchanged while this constraint holds.
