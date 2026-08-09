# Module: apps/mymatasan/services/pages.go

## Purpose

mymatasan's page catalog: the human-facing half of authorization, built on
`domain/shared/services/page_access.go`. `apps/mymatasan/services/rbac.go`'s `Policy()` remains
the complete governed API surface; this file says the same thing in the unit an administrator
actually grants — "this role may watch live video and review footage, and nothing else" —
without the administrator needing to know PTZ lives at `/api/cameras/*/ptz`.

**No behaviour change.** Nothing calls `Pages()` or `RolePresets()` outside tests. Boot still
seeds roles from `Policy()` via `EnsureRoles`/`EnsureApplianceRoles`, exactly as before; this
file is a second, provably-equivalent description of the same three built-in roles, not yet
wired to anything.

## Page catalog: Pages()

```go
func Pages() sharedservices.PageCatalog
```

- **`Baseline`** — `GET /api/auth/session`, `POST /api/auth/change-password`,
  `GET /api/setup`: the grants every role needs to complete sign-in, matching `Policy()`'s own
  first rule and the fix for the sign-in-lockout bug documented in `rbac.go.md`. Made
  un-untickable by living outside the page set entirely.
- **Grantable pages** (not `AdminOnly`):
  - `dashboard` (view) — reads `/api/vision`, `/api/notifications`, `/api/capacity`.
  - `live-views` — *view*: read cameras, refresh camera health, live-view/WebRTC offer, read
    runtime settings (the wall needs them to render). *use* (a rung above, since it changes the
    physical world): PTZ, talk-back offer.
  - `notifications` — *view*: read notifications and vision alerts. *use*: acknowledge an alert,
    mark a notification read.
  - `recordings` — *view* only, deliberately with no higher rung: reads `/api/recording`.
    Deleting footage stays superadmin-only and is never a page an administrator can hand out —
    an operator present at an incident must not be able to destroy the footage of it, and that
    property cannot survive being a checkbox.
  - `objects` — *view*: reads `/api/observations`.
- **`AdminOnly` pages** — listed so the catalog stays a complete description of the app, but
  `DerivePermissions` will never grant them regardless of what a role's rows say:
  - `cameras` → `/api/cameras`, `/api/onvif` (manage/full)
  - `detection` → `/api/vision`, `/api/anomaly` (manage/full)
  - `teach` → `/api/teach`, `/api/training` (manage/full)
  - `people` → `/api/faces` (manage/full) — the page that made the `/api/faces` gap in
    `Policy()` visible; see `rbac.go.md`.
  - `settings` → `/api/settings`, `/api/system`, `/api/pairing` (manage/full)
- **`Carveouts`** — `/api/settings/users`, `/api/settings/roles`. Required because `live-views`
  grants `GET /api/settings` (the SPA needs runtime settings to render the wall at all); without
  an explicit all-false row under each, a role with only `live-views` would be able to enumerate
  every user account and role, since the broader `/api/settings` grant would otherwise govern
  with nothing narrower to shadow it.

Page ids (`PageDashboard`, `PageLiveViews`, `PageCameras`, `PageDetection`, `PageTeach`,
`PagePeople`, `PageObjects`, `PageRecordings`, `PageNotifications`, `PageSettings`) are stable
and are what the SPA will later map to nav entries and labels. Level ids (`LevelView`,
`LevelUse`, `LevelManage`) are cumulative in that order wherever a page declares more than one.

## Presets: RolePresets()

```go
func RolePresets() map[string][]sharedservices.PageGrant
```

Maps the two grantable built-in roles onto page grants:

- `viewer` → `dashboard`(view), `live-views`(view), `notifications`(view).
- `operator` → `dashboard`(view), `live-views`(use), `notifications`(use), `recordings`(view),
  `objects`(view).

`admin` has no preset — it is the superadmin builtin and bypasses the matrix entirely, same as
in `Policy()`.

## The safety property

`TestPagePresets_MatchTodaysPolicyExactly` derives the viewer/operator presets through
`DerivePermissions` and diffs the result against `RolePermissions(..., Policy())` — the exact
rows boot seeds today — printing a `LOST`/`WIDENED`/`DIFFERS` report on any mismatch (an
all-false row present on only one side is not a difference; deny-by-default already refuses it).
`TestPagePresets_AuthorizeIdenticallyToPolicy` goes one step further and checks the REAL
`Authorize` matcher decides identically for both derivations across 20 probe paths (session,
cameras, PTZ, talk, alerts ack, notifications read, recording read/delete, observations,
settings/users/roles, onvif, system reset, and an unknown route). Together these are what make
adopting pages for viewer/operator provably a no-op today: the only way this refactor could
become a security regression is by widening a role, and both tests forbid it.

Five further tests cover the model's other invariants for this catalog specifically: carve-outs
actually deny what the broader `/api/settings` grant would otherwise expose
(`TestPages_CarveOutsDenyWhatABroaderGrantWouldExpose`); levels are cumulative — holding "use" on
Live Views implies "view" (`TestPages_LevelsAreCumulative`); an `AdminOnly` page can never be
granted even via a crafted/stale row (`TestPages_AdminOnlyPagesCannotBeGranted`); an unknown page
id degrades to "not held" rather than breaking derivation, with the baseline still granted so the
affected user can still sign in (`TestPages_UnknownPageIsIgnored`); and every path a page level
grants is one `Policy()` actually governs (`TestPages_GrantOnlyPathsThePolicyGoverns`) — this last
one is what caught the `/api/faces` gap (see `rbac.go.md`).

## Notes

- Adding a page: declare it here with the paths its levels need, add the id to the SPA's nav map
  (future work), and add it to any preset that should hold it — the existing tests will flag a
  level that grants a path `Policy()` does not govern, or a preset that stops matching the role
  it replaces.
- `canRead`/`canWrite`/`canAll` here are local aliases of `sharedservices.Read`/`Write`/`Full`
  (`page_access.go.md`), named so the catalog reads as prose; distinct from `rbac.go`'s
  `read`/`write`/`none` `Verbs` values, which describe the API-path catalog instead.
- `newMatrix` in `pages_test.go` builds a real `IAccessPermissionService` over the same in-memory
  `memPermRepo` fake `rbac_test.go` uses, so these tests exercise the real matcher rather than
  re-implementing its precedence rules.
