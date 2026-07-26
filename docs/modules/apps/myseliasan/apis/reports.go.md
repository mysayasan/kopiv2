# Module: apps/myseliasan/apis/reports.go

## Purpose

HTTP surface for the on-demand printable PDF reports (`services.IReportService`). Every
route streams a rendered PDF as an attachment (never JSON), so the browser downloads/opens
it directly; the frontend's `ReportsPage` instead `fetch()`es the bytes into a blob and
shows them in an in-page preview modal (see `apps/myseliasan/README.md` -> "Reports").

## Endpoints

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/reports/fleet-health.pdf?range=<days>` | Online/offline status of every node, certificate expiry roster, and an alert summary over the trailing `range` days (default 30, via `queryDays`). |
| `GET` | `/api/reports/inventory.pdf?siteId=<id>` | Asset register per building: rendered floor plans with camera placements and on-site appliances. `siteId` omitted/`0` = every site; `>0` narrows to one. |
| `GET` | `/api/reports/security.pdf?range=<days>` | Users, roles, the endpoint permission matrix, and the audit trail over `range` days, plus a data-protection attestation paragraph. **Superadmin only** (`requireSuper`). |
| `GET` | `/api/reports/incident.pdf?range=<days>&notificationId=<id>` | Recent alerts over `range` days with per-event detail and an inline snapshot when the event metadata carries one; `notificationId>0` narrows to a single event instead of the range. |

## Authorization

- `auth.Middleware` + `session.Middleware` on the `/reports` subrouter -- same as every other
  control-plane API; a signed-in session is required for all four routes.
- `requireSuper` wraps only the security route: it checks
  `AccessSessionMidware.IsSuperadmin(r)` and returns `ErrLimitedAccess` (403) otherwise. The
  security report exposes users, the permission matrix, and the audit trail, so it is gated
  the same way `GET /api/audit` is (`apis/audit.go.md`).
- Not seeded as an `api_endpoint` row in `app.go`'s `Seeders` (unlike `Audit`/`Notifications`/
  etc.) -- the group is gated purely by session auth (+ the in-handler superadmin check for
  security), not the accessrbac permission-matrix catalog.

## Constructor

`NewReportsApi(router, auth, session, svc, audit)` -- mounts the four `GET` routes on a
`/reports` subrouter. `svc` is `services.IReportService`; `audit` is
`services.IAuditService`, used by `deliver` (below). Registered in `app.go`'s
`RegisterAppRoutes` right after `NewNotificationApi`, with `reportService` built from
`registry`, `siteService`, `notificationService`, `auditService`, `userService`,
`roleService`, and `deps.AccessPerms`.

## `deliver` -- response framing + audit trail

Every handler calls the shared `deliver(w, r, action, rep, err)`:

- On `err != nil`, sends a JSON `ErrInternalServerError` (like other endpoints) -- reports
  never partially stream on a build failure.
- On success, writes `Content-Type: application/pdf`, `Content-Disposition: attachment;
  filename="<rep.Filename>"`, `Content-Length`, and `X-Content-Type-Options: nosniff`, then
  writes `rep.Data` directly.
- Before writing the response, if `audit != nil`, records an entry via
  `services.IAuditService.Record` -- `Action` is one of `report.fleet_health`,
  `report.inventory`, `report.security`, `report.incident`; `ActorId`/`ActorEmail`/
  `ActorRole` from `auditActor(r)` (`apis/audit.go.md`'s shared helper); `TargetType:
  "report"`, `TargetId: rep.Filename`; `ClientIp` from `clientIP(r)`. Generating a report is
  a sensitive read of fleet-wide data (in the security report's case, of RBAC/audit data
  too), so every generation -- not just a write -- leaves a trail.

## `queryDays`

Parses a `range` (or other named) query param as an int; a missing/unparseable/`<=0` value
defaults to `30`. The upper bound (366 days) is enforced inside `services.normDays`, not
here.

## Notes

- Response bytes are produced entirely by `services.IReportService`; this file owns only
  routing, the superadmin gate, response framing, and the audit write.
- `reports_test.go` exercises `deliver`/`requireSuper` directly against a stub
  `IReportService` (bypassing the auth/session middleware, which needs a full RBAC stack),
  proving the PDF content-type/disposition/body and the one-audit-entry-per-generation
  invariant, and that the security route denies a nil/non-superadmin session.
