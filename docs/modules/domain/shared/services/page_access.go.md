# Module: domain/shared/services/page_access.go

## Purpose

The shared page-level access model: the unit an administrator actually grants (a PAGE, at a
LEVEL) and the pure function that turns what they granted into the `AccessRolePermission` rows
the server already enforces.

Two independent sources of truth motivate this. mymatasan's nav rail hides entries with an
`isAdmin` bool on the user while the server authorizes from the permission matrix — nothing
keeps those two in agreement. And the matrix itself (`/api/cameras/*/ptz` + four verbs) is an
engineer's model, not a question an administrator can answer, and mymatasan has no role
administration at all: a site cannot express "Night guard: live views, recordings and
notifications, nothing else." A page catalog fixes both — an administrator grants pages, the
matrix is *derived* from that, and (later) the nav rail is rendered from the same page set, so
the menu and the enforcement stop being two things that can disagree.

**This file adds no behaviour.** Authorization still consults `AccessRolePermission` exactly as
before — no new middleware, no new way to fail open. It is the model and its derivation function
only; nothing calls `DerivePermissions` outside tests yet.

## Key Types

```go
type PathGrant struct {
    Path                                    string
    CanGet, CanPost, CanPut, CanDelete      bool
}
func Read(path string) PathGrant   // CanGet only
func Write(path string) PathGrant  // CanPost only
func Full(path string) PathGrant   // every verb
```

`PathGrant` is one API path and the verbs a page level needs on it. `Read`/`Write`/`Full` are
the named constructors a catalog is written against.

```go
type PageLevel struct {
    Id     string        // "view", "use", "manage" — stable, stored against a role
    Grants []PathGrant    // what this rung ADDS on top of the rung before it
}
```

Levels are **cumulative** and must be declared in increasing order: holding a level implies
every level before it. That is what makes "manage without view" unrepresentable rather than
merely discouraged, and it turns derivation into a prefix-take instead of a set union an author
has to get right by hand.

```go
type Page struct {
    Id        string       // stable, stored against a role, maps to a nav entry in the SPA
    Group     string       // nav group ("workspace", "surveillance", ...)
    Order     int
    Levels    []PageLevel  // increasing order of access
    AdminOnly bool         // listed for completeness, but can never be granted
}
```

`AdminOnly` marks a surface (camera/detection config, settings, user management, ...) that only
a superadmin may ever hold. It stays in the catalog — a page catalog that omits an area is an
area nobody can see they are not granting, the exact defect `apps/mymatasan/services/rbac.go`
already guards against for the API-path catalog — but `DerivePermissions` refuses to grant it
even if a stale row somehow names it.

```go
type PageCatalog struct {
    Pages     []Page
    Baseline  []PathGrant  // granted to EVERY role, independent of any page
    Carveouts []string     // paths that always get an explicit all-false row unless a held page names them
}
```

- **`Baseline`** exists because some access is not about a page at all — it is about being
  signed in. Reading your own session, changing your own password, checking setup state: a role
  without these cannot get through the sign-in sequence to reach any page, so they must not be
  something an administrator can accidentally untick. This directly encodes the sign-in-lockout
  defect fixed in the previous commit (`/api/auth/session` missing from `Policy()` locked out
  `viewer`/`operator` entirely).
- **`Carveouts`** exist because the underlying matcher takes the MOST SPECIFIC matching rule and
  rules do not union (see `access_rbac.go.md`). A role granted `GET /api/settings` (because a
  page needs it to render) with no row at all for `/api/settings/users` can enumerate every
  account — the broader grant governs because nothing narrower exists to carve it out. An
  all-false row is the only way to say "…but not this part." Getting this list wrong is the one
  way this package can hand out access nobody granted.

```go
type PageGrant struct {
    PageId string
    Level  string
}
```

One role holding one page at one level — what an administrator actually chose, and what is
persisted in `access_role_page` (`entities.AccessRolePage`).

## Key Function: DerivePermissions

```go
func DerivePermissions(roleId int64, catalog PageCatalog, held []PageGrant) []entities.AccessRolePermission
```

Turns what an administrator chose into the matrix rows the server enforces:

1. Adds `catalog.Baseline` unconditionally.
2. For every held `PageGrant`, walks that page's levels in order, unioning each level's grants
   (verbs merged per path with logical OR) until — inclusive — the held level; an unknown page
   id or an `AdminOnly` page is skipped.
3. Applies `catalog.Carveouts` last, writing an explicit all-false row for any carve-out path
   nothing above already granted.
4. Returns the rows sorted by path, each marked `Managed: true`.

The result is the **complete** managed matrix for that role — callers are expected to replace
the role's managed rows with it wholesale (see `role_page.go`), which is what makes removing a
page actually remove the access instead of leaving an orphaned grant behind. An unknown page id
or level is ignored rather than erroring, because a catalog can lose a page across an upgrade and
a role still holding a stale row for it should degrade to not having it, not break the boot.

## Other helpers

- `PageCatalog.Page(id)` — look up a page by id.
- `PageCatalog.Sorted()` — pages in display order (`Group`, `Order`, `Id`).
- `Page.HighestLevel()` / `Page.HasLevel(id)` — used by callers validating a requested level.

## Notes

- Lives in `domain/shared/services` (not per-app) because the model, the level cumulativity
  rule, and the derivation algorithm are identical for every app; only the catalog content
  (`apps/mymatasan/services/pages.go`) is per-app.
- Consumed by `role_page.go` (`IAccessRolePageService`), which is the only place
  `DerivePermissions` is called from application code today.
- Proven a faithful reproduction of mymatasan's existing catalog by
  `apps/mymatasan/services/pages_test.go` — see `pages.go.md`.
- Nothing in `infra/apphost` or any app's boot sequence calls this yet; wiring page-derived
  permissions into boot and rendering the nav rail from pages is later work.
