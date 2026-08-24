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
  - `recordings` — *view*: reads `/api/recording`. *use* (a second rung, added for evidence
    export): grants `/api/evidence` for GET **and** POST (`canUse`, not `canWrite`) —
    exporting a verifiable bundle. The GET is not a widening: a bundle is built
    asynchronously, so the exporter must poll the job and then download the file. Granted
    POST alone, the rung conferred the right to START an export and was then refused both the
    status and the result — the capability it describes, unusable. Found in W3-3 while tracing
    the path for the case bundle, not by a test. Deleting footage stays
    superadmin-only and is still never a level this page can hand out — an operator present
    at an incident must be able to hand the footage of it to somebody, and must not be able
    to destroy it. That is the same line drawn twice, and every export is audited with the
    operator's stated reason, which is what makes granting `use` safe rather than merely
    convenient.
  - `objects` — *view*: reads `/api/observations`.
  - `cases` (W3-3) — *view*: reads `/api/cases`. *use*: `canUse("/api/cases")` — opening,
    annotating, closing and exporting case files. It sits in the workspace group beside
    Recordings because it is the same job: reviewing what happened. Exporting a case is an
    evidence export and is audited as one, so the rung carries what the Recordings rung
    carries; a role given Cases-use without Recordings-use can still export its own cases,
    which is the point of the grant living here too. No level grants DELETE, so removing a
    case entirely stays with an administrator — a case is the record that an investigation
    happened.
- **`AdminOnly` pages** — listed so the catalog stays a complete description of the app, but
  `DerivePermissions` will never grant them regardless of what a role's rows say:
  - `cameras` → `/api/cameras`, `/api/onvif` (manage/full)
  - `detection` → `/api/vision`, `/api/anomaly` (manage/full)
  - `teach` → `/api/teach`, `/api/training` (manage/full)
  - `people` → `/api/faces` (manage/full) — the page that made the `/api/faces` gap in
    `Policy()` visible; see `rbac.go.md`.
  - `settings` → `/api/settings`, `/api/system`, `/api/pairing` (manage/full)
  - `audit` (`PageAudit`) → `/api/audit` (view only, at every level — the API has no
    mutating route to grant). Its own page rather than a corner of Settings, because it
    answers a different question from every other screen: not "how is this configured" but
    "what did people do", and it is what an auditor is actually sent to look at.
- **`Carveouts`** — `/api/settings/users`, `/api/settings/roles`. Required because `live-views`
  grants `GET /api/settings` (the SPA needs runtime settings to render the wall at all); without
  an explicit all-false row under each, a role with only `live-views` would be able to enumerate
  every user account and role, since the broader `/api/settings` grant would otherwise govern
  with nothing narrower to shadow it.

Page ids (`PageDashboard`, `PageLiveViews`, `PageCameras`, `PageDetection`, `PageTeach`,
`PagePeople`, `PageObjects`, `PageRecordings`, `PageCases`, `PageNotifications`,
`PageSettings`, `PageAudit`) are stable and are what the SPA will later map to nav entries and labels. Level
ids (`LevelView`, `LevelUse`, `LevelManage`) are cumulative in that order wherever a page
declares more than one.

## Presets: RolePresets()

```go
func RolePresets() map[string][]sharedservices.PageGrant
```

Maps the two grantable built-in roles onto page grants:

- `viewer` → `dashboard`(view), `live-views`(view), `notifications`(view).
- `operator` → `dashboard`(view), `live-views`(use), `notifications`(use), `recordings`(**use**,
  was `view` — the built-in operator preset gained evidence-export capability alongside the
  new `recordings` `use` level), `objects`(view), `cases`(use).

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
