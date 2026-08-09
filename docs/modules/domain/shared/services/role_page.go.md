# Module: domain/shared/services/role_page.go

## Purpose

`IAccessRolePageService` owns what an administrator chose — which pages a role holds, at which
level — and keeps the enforced permission matrix (`AccessRolePermission`) derived from it in one
operation, so the navigation (built from pages) and the server (built from the matrix) cannot
drift into two independent sources of truth.

**No caller wires this in yet.** It is unit-tested in isolation (`role_page_test.go`) against an
in-memory repo; nothing in boot or any API handler constructs or calls it in this commit.

## Key Type

```go
type IAccessRolePageService interface {
    ListForRole(ctx context.Context, roleId int64) ([]PageGrant, error)
    SetForRole(ctx context.Context, roleId int64, catalog PageCatalog, grants []PageGrant) error
    AdoptPreset(ctx context.Context, roleId int64, catalog PageCatalog, preset []PageGrant) error
}
```

- **`ListForRole`** — the role's currently held `(PageId, Level)` pairs, read from
  `access_role_page`.
- **`SetForRole`** — replaces the role's page rows wholesale and re-derives its managed
  permission rows in the same call. Deletes all of the role's existing `access_role_page` rows,
  inserts one row per grant (silently skipping an unknown page id, an `AdminOnly` page, or a
  level the page does not declare — an admin-only page or bad level would derive to nothing and
  leave the UI claiming access that does not exist), then calls the internal `rederive`.
- **`AdoptPreset`** — one-shot boot adoption for the built-in roles. Gives a role its preset
  pages, but only if the role has never been page-managed before.

## rederive (internal)

Replaces the role's `Managed == true` permission rows with `DerivePermissions(...)` of its
current pages; any row with `Managed == false` (typed in by hand under "Advanced" in the RBAC UI)
is left completely untouched and is never re-derived over. This is the property that makes the
feature trustable: ticking a page can never silently destroy an administrator's deliberate
exception.

## AdoptPreset — the upgrade path

Distinguishes three states for a role:

1. **Already page-managed** (has any `Managed == true` permission row) — never re-impose the
   preset; an administrator's choices, including "holds zero pages" (used to suspend a role
   without deleting it), must never be overwritten by a later boot.
2. **Pre-existing unmanaged rows from an install that predates page-level access** — every such
   row came from the old policy catalog, so it *is* logically managed; the column just did not
   exist when it was written. `AdoptPreset` claims each one (sets `Managed = true` via
   `perms.Set`) before calling `SetForRole` with the preset, so the first derivation replaces
   them instead of layering a second, overlapping set of rows on top.
3. **Genuinely fresh role, no rows at all** — proceeds straight to `SetForRole(preset)`.

## Notes

- Constructed via `NewAccessRolePageService(db, perms)` (real `dbsql` repos) or
  `NewAccessRolePageServiceWithRepos(repo, permRepo, perms)` (test/DI seam used by
  `role_page_test.go`).
- Depends on `IAccessPermissionService` (`access_rbac.go.md`) for `ListForRole`/`Set` on the
  permission side, and a `dbsql.IGenericRepo[entities.AccessRolePage]` for the page side.
- Table is `access_role_page` (`entities.AccessRolePage`, `domain/entities/access_rbac.go.md`).
- Tested by `role_page_test.go` against an in-memory `IGenericRepo` fake (not the real database)
  for both `AccessRolePage` and `AccessRolePermission`, so the tests exercise the real
  delete-and-rewrite/derive logic rather than a re-implementation of it. Seven cases: deriving
  the matrix from a page grant, removing a page removing its access, a hand-edited row surviving
  a page edit, idempotence across repeated saves, refusing an admin-only-page/unknown-page/bad-
  level grant, the upgrade path claiming pre-existing unmanaged rows without duplicating them,
  and adoption never overriding a role that has already been page-managed (including the
  "administrator removed every page" case).
