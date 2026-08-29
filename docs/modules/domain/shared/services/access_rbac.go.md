# Module: domain/shared/services/access_rbac.go

## Purpose

Implements the shared "accessrbac" RBAC core: role CRUD (`accessRoleService`) and a per-endpoint permission matrix (`accessPermissionService`). Both services are app-agnostic and have no `app_code` dimension.

## Types

### AccessPrincipal

The app-agnostic view of an authenticated user that the RBAC middleware needs:

```
RoleId             int64
Disabled           bool
MustChangePassword bool
MustEnrollMfa      bool
```

`MustEnrollMfa` (Productization Phase 3) pins a user to second-factor enrollment the same
way `MustChangePassword` pins them to the password-change form — set when the app's MFA
policy requires a factor for this account's role and none is confirmed yet. Safe to
enforce **after** a session already exists: the password has already succeeded, and the
user is being made to *add* a factor, not to prove one they do not have (the alternative —
refusing the login outright — would lock out every existing admin the moment a
`required` policy is switched on). An app with no MFA policy (or one that has not wired
this field) simply leaves it `false`. Apps map their own user record to this via
`AccessUserResolver`.

### AccessUserResolver (interface)

```go
ResolveAccessUser(ctx context.Context, userId int64) (*AccessPrincipal, error)
```

Returning `(nil, nil)` means "no such user" (treated as signed-out).

## Role Service (IAccessRoleService)

- `EnsureBuiltins(ctx)` — seeds `superadmin` (IsSuperadmin=true, Builtin=true) and `viewer` (Builtin=true) on startup, skipping existing rows.
- `GetByName / GetById` — look up a role; returns `nil, nil` when not found. `GetById` uses the primary key (`repo.GetById`), not `GetByUnique` — the prior use of `GetByUnique(ctx,"","id",id)` matched no field (no `ukey:"id"` tag) and always returned the first role (the superadmin), making every role lookup resolve as superadmin.
- `List` — returns all roles (up to 1000).
- `Create(name, description)` — trims, validates uniqueness, inserts, returns the new row.
- `Update(id, name, description)` — updates mutable fields (name, description, UpdatedAt).
- `Delete(id)` — rejects built-in roles; deletes others.

## Permission Service (IAccessPermissionService)

- `EnsureViewerDefaults(ctx, viewerRoleId)` — enforces least privilege for the viewer role. Early builds seeded viewer with a read-everything `GET /api` wildcard that exposed every administrative surface to any viewer. This method now **strips** that legacy row on startup (matching only the exact seed shape: GET-only on `/api`). Viewer starts with no permissions; an admin grants specific read paths via the RBAC matrix. Intentional narrower grants are left untouched.
- `Authorize(ctx, roleId, path, method)` — matches the role's permission rows against `path` via `accessPathMatches` (segment-wise, see below), and the **most specific** matching row decides (`accessMoreSpecific`, not a union) — no match = `false`. Methods `GET/HEAD/OPTIONS` check `CanGet`; `POST` checks `CanPost`; `PUT/PATCH` check `CanPut`; `DELETE` checks `CanDelete`. A more specific row can **shadow** a broader grant — e.g. `/api/settings` readable, `/api/settings/users` (deeper, more specific) not — so a matrix must be read specificity-first, not top-to-bottom; this is the mechanism carve-outs rely on.
- `ListForRole(ctx, roleId)` — returns all permission rows for a role (up to 1000), **sorted by `Path` ASC** for stable ordering. A stable order prevents the just-edited row from reshuffling in the RBAC matrix UI when its checkbox is toggled.
- `Set(ctx, perm)` — upsert by `(roleId, path)`: updates verb flags if the path already exists, inserts otherwise. Normalizes the path (leading slash, no trailing slash, `/` for root). **Refuses a root path (`"/"`)**: `len(accessSegments(perm.Path)) == 0` returns an error instead of writing the row. A root-path row was a real, undefended grant-everything wildcard that an admin could create through the management API and that looked like any other row in the matrix UI — a role that should have everything is a superadmin (an explicit, visible bypass flag), not a role with a magic row in it.
- `Delete(ctx, id)` — deletes a permission row by ID.

## Path Matching

`accessPathMatches(allowed, requestPath)` matches **segment-wise**, and a `"*"` segment in
`allowed` matches exactly one path segment. This is what makes an action permission
expressible at all: REST routes put the action after the id
(`/api/cameras/7/ptz/move`), so a pure string prefix can't see past `/api/cameras` — there
was no way to let a role move a camera without also letting it create one.
`"/api/cameras/*/ptz"` says exactly what is meant. Segment-wise matching also closed a
smaller hole: as a raw string prefix, `/api/node` matched `/api/nodes-secret`; it no longer
does. The empty-segment case (`allowed == "/"`) still matches everything, which is why `Set`
refuses to store it.

`accessMoreSpecific(a, b)` replaces a former raw string-length comparison for picking the
winning row when more than one matches. **More segments wins** (a deeper rule beats a
shallower one regardless of character count — the old length comparison could rank a
longer-but-shallower path above a genuinely deeper one). **On a tie, more literal (non-`*`)
segments wins**, so `/api/cameras/*/ptz` beats `/api/cameras/*/*` — a rule that names the
action is more specific than one that wildcards it.

`PathGoverns(rulePath, requestPath)` is `accessPathMatches`, **exported so an app's policy catalog
can be tested against the routes that app actually serves.** A catalog is prose until something
checks it against the router: `"/api/doors/unlock"` reads like the rule that lets an operator open a
door and cannot match `"/api/doors/7/unlock"` — three segments against four — so on mypintusan the
operator role's entire reason to exist was denied on every install, silently, because nothing ever
asked the matcher whether the rule matched anything. Exporting the matcher is what turns *"does this
rule govern a real route?"* into an assertion instead of a careful read. See
`apps/mypintusan/services/rbac_test.go` for the pattern: a list of the paths the app serves, checked
against the catalog in both directions.

## Constants

- `RoleSuperadmin = "superadmin"` — the name of the built-in superadmin role.
- `RoleViewer = "viewer"` — the name of the built-in read-only role.

## Notes

- Both services are constructed once in `infra/apphost` and passed to apps via `deps.AccessRoles` / `deps.AccessPerms`.
- The enforcement middleware is `domain/utils/middlewares.AccessSessionMidware`.
- The management API surface is `domain/shared/apis/access_rbac.go`.
- `page_access.go` and `role_page.go` (same package) add a page-level model that DERIVES rows
  for `accessPermissionService.Set` from what pages a role holds, rather than an admin editing
  `AccessRolePermission` rows directly. `Authorize`/`Set`/`ListForRole` above are unchanged and
  are exactly what a page-derived row is still enforced through — see `page_access.go.md` and
  `role_page.go.md`. Nothing calls the page-level services outside their own tests yet.
