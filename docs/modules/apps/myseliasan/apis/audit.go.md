# Module: apps/myseliasan/apis/audit.go

## Purpose

Read-only, superadmin-gated HTTP surface over the append-only audit trail (`services.IAuditService`). The trail itself has no create/update/delete route here — entries are written server-side by the sensitive-action handlers in `nodes.go`, `rbac_admin.go`, `node_access_api.go`, and `node_proxy.go`.

## Endpoints

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/audit?limit=&offset=&action=&outcome=&actorEmail=&targetType=&targetId=&from=&to=` | List audit entries newest-first. All query params optional; `limit`/`offset`/`from`/`to` parse failures are treated as `0`. **Superadmin only.** `outcome`, `actorEmail`, `from`/`to` are new — they came free with the move to the shared `domain/shared/audit` trail (`services/audit.go.md`); myseliasan's own `List` previously only offered `action`/`targetType`/`targetId`. |

## Authorization

- `auth.Middleware` + `session.Middleware` on the `/audit` subrouter.
- `requireSuper` additionally checks `AccessSessionMidware.IsSuperadmin` and returns `ErrLimitedAccess` (403) otherwise — the audit trail can expose sensitive operator activity (who adopted/released nodes, changed roles, rotated the fleet key, sent mutating tunneled commands), so only superadmins may read it.

## Constructor

`NewAuditApi(router, auth, session, audit)` — mounts the `GET /audit` route on its own subrouter. Registered in `app.go` alongside `NewRbacAdminApi`.

## Shared helpers

This file also defines two small helpers reused by every sensitive-action handler across the package:

- `auditActor(r)` — pulls `(actorId, actorLabel, roleId)` from the request's JWT claims (`enumauth.Claims`): label prefers `Email`, falls back to `Name`, then the numeric id.
- `clientIP(r)` — best-effort source address: first hop of `X-Forwarded-For` when present, else the host part of `RemoteAddr`.

## Notes

- Seeded as an `api_endpoint` row in `app.go`'s `Seeders` (`Title: "Audit"`, `Path: /api/audit`, `AccessTier: AuthOnly`) for rate-limiting/runtime metadata; the superadmin gate is enforced in-handler, not by the endpoint catalog.
- `list` returns a paging envelope (`controllers.SendPagingResult`) matching the shape other list endpoints use, so the SPA's `DataTable` can consume it directly.
