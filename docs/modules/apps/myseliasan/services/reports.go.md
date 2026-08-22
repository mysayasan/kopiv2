# Module: apps/myseliasan/services/reports.go

## Purpose

Implements `IReportService`, the four on-demand printable-PDF report builders behind
`apis/reports.go`. Each gathers data from the existing fleet services (node registry, site
service, notification feed, audit log, RBAC) and renders it through the shared
`domain/report` builder (`domain/report/doc.go.md`) into a ready-to-stream `Report{Filename,
Data}`.

## Constructor

`NewReportService(registry, sites, notif, audit, users, roles, perms, briefer)`:

| Param | Type | Used for |
|---|---|---|
| `registry` | `INodeRegistry` | `FleetHealth`'s `FleetStatus`/`List`; `Inventory`'s per-site resident-node lookup. |
| `sites` | `ISiteService` | Every report's site list; `Inventory`'s floor plans + placements. |
| `notif` | `*notification.Service` (accepted as the concrete type, stored behind the internal `notifLister` interface — just `List`) | `FleetHealth`'s alert summary; `Incident`'s event list. |
| `audit` | `IAuditService` | `Security`'s audit-trail section. |
| `users` | `IControlUserService` | `Security`'s user roster. |
| `roles` | `sharedservices.IAccessRoleService` | `Security`'s role list + permission-matrix section. |
| `perms` | `sharedservices.IAccessPermissionService` | `Security`'s per-role endpoint grants. |
| `avail` | `INodeAvailabilityService` (may be `nil`) | `FleetHealth`'s Availability section. `nil` omits the section entirely. |
| `briefer` | `reportBriefer` (may be `nil`) | `executiveSummary`'s AI briefing section on `FleetHealth` and range-scoped `Incident`. |

`now time.Time` is a parameter on every builder (never read from the clock), so the header
stamp and every `rangeDays` calculation are deterministic and unit-testable.

## `reportBriefer` and `executiveSummary` — the AI briefing section

`reportBriefer` is the one-method sliver of the fleet AI agent the reports use:
`GenerateBriefing(ctx, windowHours) (Briefing, error)` (`services/agent_digest.go.md`). `app.go`
wires the real `*DigestService` in; `nil` disables the section entirely and every report renders
exactly as it did before this existed — `briefer == nil` is the fast, explicit early return in
`executiveSummary`.

`executiveSummary(ctx, doc, rangeDays)` is called at the top of `FleetHealth` (always) and
`Incident` (only when `notificationID == 0` — the range-scoped report; a single-event incident
report's own detail section already **is** the summary, so it is skipped there). It renders an
`H1 "Executive Summary"`: the LLM `Narrative` (when the briefing reached a model) followed by a
`Note` naming the model and stressing that every number below is computed deterministically, then
an `H2 "Findings"` bulleted list of the deterministic `Lines` regardless of whether a narrative
rendered. A briefing error, or a briefing with nothing to say, skips the whole section silently —
best-effort, same invariant as the digest itself: a briefing failure costs the section, never the
report.

**The narrative is requested in English** even when the digest's own configured language is
`ms`/`zh`/`ar` — `GenerateBriefing` fixes this, not `reportService`, since `domain/report` renders
cp1252/Helvetica text and cannot carry non-Latin glyphs (see `services/agent_digest.go.md`).

## `IReportService`

| Method | Report |
|---|---|
| `FleetHealth(ctx, now, rangeDays)` | Live online/offline + certificate roster + alert summary over the trailing `rangeDays` (default 30 via `normDays`). |
| `Inventory(ctx, now, siteID)` | Asset register per building with rendered floor plans + camera placements. `siteID == 0` = the whole fleet; `>0` narrows to one site. |
| `Security(ctx, now, rangeDays)` | RBAC users/roles/permission matrix + audit trail over `rangeDays` + a data-protection attestation paragraph. Superadmin-gated at the API layer, not here. |
| `Incident(ctx, now, rangeDays, notificationID)` | Recent alerts over `rangeDays` with per-event detail (and an inline snapshot when one is carried in the event's metadata). `notificationID > 0` narrows to a single event. |

## Fleet Health

`H1 "Fleet Summary"` stat tiles (Total/Online/Lost/Self-dropped/Certs expiring/Certs
expired, the danger-tinted ones sourced straight from `INodeRegistry.FleetStatus`) plus a
note naming the cert-warn window — which used to end "historical uptime is not yet tracked"
and now points at the section below.

`H1 "Availability"` (`availabilitySection`, W2-2) — what the fleet's uptime WAS over the
reporting period, from `INodeAvailabilityService` (`node_availability.go.md`). Stat tiles
(fleet availability, total downtime, outages, monitoring coverage), a note that spells out how
much node-time was actually OBSERVED and how much the control plane itself was not watching,
then `By node`, `By site` (only when there is more than one site group — one group is the
fleet restated) and `By month`. Availability and coverage are always printed together: an
availability of 100 over a coverage of 4 is not a good month. `"no data"` is spelled out
rather than rendered as `0.00%`, since the two mean opposite things. Best-effort — a nil
service or a failure costs the section, never the report, so the PDF renders exactly as it did
before W2-2 when history is unavailable.

`H1 "Nodes"` — every adopted node, name-sorted, one row
per node (site, `statusLabel`, last-seen, cert-expiry, auto-renew). `H1 "Alerts (last N
days)"` — counts from `recentNotifications` (capped at the feed's 500-row read) grouped by
category and by the top-10 noisiest `Source`.

## Inventory

One `H1` block per site (icon + name), each on `doc.AddPage()` after the first, with a
`KeyValues` block (kind, description, coordinates when placed) and an "Appliances on site"
table of nodes whose `SiteId` matches. A site whose kind has no plans
(`!entities.HasPlans(s.Kind)` — a point asset) gets a note and skips straight to the next
site. Otherwise every floor is fetched via `ISiteService.SiteFloorplans`, ordinal-sorted,
and each gets its **own page** (`H1` = `"<site> — <floor>"`) with:

- `renderFloorPlan` — decrypts the plan image (`ISiteService.FloorImage`) and composites the
  camera pins + the authored wall/door/window/stairs geometry via
  `renderFloorPlacements`/`renderFloorGrid` (`report_floorplan.go.md`/
  `report_floorgrid.go.md`), embedding the result via `doc.Image`. Any failure — no image,
  undecodable bytes — is surfaced as a note on the page (`"Floor plan image could not be
  shown: <reason>"`), never dropped silently: an absent plan is otherwise
  indistinguishable from "this floor has no plan".
- A table of the floor's placements (name, camera/node kind, coverage `<fov>° @ <heading>°`,
  mount height).

## Security

`H1 "Users"` (name/kind/role/state/last-login, sorted by `userLabel`), `H1 "Roles"`
(name/superadmin/built-in/description), `H1 "Permission Matrix"` — one `H2` sub-section per
role: a superadmin role gets a one-line "unrestricted" note (its access is a bypass, not
rows); every other role's endpoint grants come from `perms.ListForRole`, path-sorted, GET/
POST/PUT/DELETE as bullet ticks. `H1 "Audit Trail (last N days)"` reads up to 500 entries
via `audit.List(ctx, 500, 0, AuditFilter{From: from})` — the window is applied by the QUERY
now, not filtered afterwards in Go: the previous `List(ctx, 500, 0, "", "", "")` fetched the
newest 500 rows unconditionally and then kept only `CreatedAt >= from`, so a busy period
outside the report's window could push in-window entries off the end of those 500 and
silently shorten the report's trail. `H1 "Data Protection"` is a fixed
attestation paragraph describing AES-256-GCM at-rest encryption of fleet secrets and
floor-plan images, and the audit trail's append-only, tamper-evident nature.

## Incident

The executive summary (above) renders only when `notificationID == 0` — the ranged report.
`H1 "Events"`; with `notificationID > 0`, filters to that single event, else to
`CreatedAt >= from` over the trailing `rangeDays`, from the same 500-row feed read as Fleet
Health's alert summary. Each event gets an `H2` (`"#<id>  <title-or-category>"`), a
`KeyValues` block (time/category/severity/source), its body paragraph, and — via
`snapshotFromMetadata` + `decodeImageBytes` — an inline `doc.Image` when the notification's
JSON `Metadata` carries an `image`/`snapshot`/`thumbnail`/`imageData` key holding a data URI
or bare base64 string. A closing note clarifies that camera snapshots otherwise live on the
recording node and are not fetched here.

## Notes

- `snapshotFromMetadata`/`decodeImageBytes` never error out to the caller on a bad/missing
  image — a snapshot is opportunistic, not required, for the incident report to render.
- `sortedCountRows`/`topCountRows` are the shared count→table helpers behind Fleet Health's
  "by category"/"noisiest sources" breakdowns.
- `kindLabel`/`nodeKindLabel`/`statusLabel`/`yesNo`/`tsFmt`/`orDash`/`tick` are small
  operator-facing label formatters shared across all four builders, kept in this file rather
  than duplicated per report. `nodeKindLabel` maps `"iot"` → "Sensor hub", `"door"` → "Door
  controller" (`mypintusan`), and `"camera"`/empty → "Camera node" (the empty case covers a node
  adopted before the `Kind` field existed).
- Every builder's final step is `doc.Output()`; the returned `*Report`'s `Filename` embeds
  `now.Format("20060102")` (e.g. `fleet-health-20260726.pdf`), matching what `apis/
  reports.go`'s `deliver` sets as the `Content-Disposition` filename.
- `reports_test.go` covers the builders directly against fake `registry`/`sites`/`notif`/
  `audit`/`users`/`roles`/`perms` implementations (no DB), including the floor-plan render
  failure path surfacing as a note rather than an error.
