# Module: apps/myidsan/apis/audit.go

## Purpose

Superadmin-only, read-only HTTP surface over `services.IAuditService`
(`services/audit.go.md`): list and CSV-export the append-only security trail. Also holds
the shared actor/context helpers every audited handler in this package uses.

## Routes

`NewAuditApi` mounts `/audit`, gated `auth.Middleware` + `access.Middleware` +
`access.RequireSuperadmin` — the trail names who did what from where, and on an identity
server it also reveals which usernames exist, so it is not delegatable through the RBAC
matrix any more than backup or user-credential management are.

- `GET /api/audit?limit=&offset=&action=&outcome=&actorEmail=&targetType=&targetId=&from=&to=`
  (`list`) — paginated listing via `auditFilterFromQuery` + `IAuditService.List`.
- `GET /api/audit/export.csv?<same filters>` (`exportCSV`) — the identical filter parser
  feeds both routes, so an export always contains exactly what the screen was showing.
  Capped at `auditExportMaxRows = 50000`; narrow the date range to get the rest. Columns:
  `timestamp_utc, action, outcome, actor_id, actor_email, actor_role, target_type,
  target_id, detail, client_ip, user_agent, metadata`.

There is deliberately no create/update/delete route: entries are written server-side by the
handlers that perform the audited actions, and nothing in the product can edit the trail.

## `csvSafe`

Defuses spreadsheet formula injection. Excel and Sheets evaluate a cell beginning with
`= + - @` (or a leading tab/CR) as a formula, and this export carries attacker-influenced
text — a failed-login "username" or a `User-Agent` is whatever the caller sent. Prefixing a
leading quote (`'`) forces a literal cell without altering what a human reads. Covered by
`apis/audit_csv_test.go.md`.

## Shared actor/context helpers

Used by every API file that audits an action (`backup.go`, `directory_config.go`,
`login.go`, `mfa.go`, `password_reset.go`, `session.go`, `stepup.go`, `user_login.go`):

- `auditActor(r)` reads the requesting operator's identity off the JWT claims
  (`(id, label, role)`); returns zeroes for an unauthenticated request — correct for a
  failed sign-in, where the caller has no identity yet and the attempted username is
  recorded as `ActorEmail` by the caller instead.
- `auditContext(r, trusted)` returns `(clientIP, userAgent)` via the shared
  `middlewares.ClientIP`/`UserAgent` (`domain/utils/middlewares/clientip.go.md`).
- `auditRecorder` is a small struct (`audit services.IAuditService`, `trusted
  []*net.IPNet`) meant to be **embedded** by handlers that perform auditable actions, via
  `newAuditRecorder(audit, trustedProxies)`. Its `record(r, e)` method fills in the actor
  from the request's claims (unless already set — a caller can override the actor for an
  event raised on someone else's behalf) and the request-derived client context, then
  writes the entry; it is a no-op when `audit` is `nil`. Centralizing this avoids the three
  things easy to get subtly different across handlers — actor lookup, address resolution,
  and the nil-service guard — where "different" would mean the same operator appears under
  two identities in the trail.

## Notes

- Mounted from `apps/myidsan/app/app.go`'s `RegisterAppRoutes` (`app/app.go.md`), alongside
  the other privilege-affecting admin surfaces.
