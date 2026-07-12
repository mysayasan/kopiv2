# Module: apps/myseliasan/services/audit.go

## Purpose

Implements `IAuditService`, the append-only audit trail for sensitive myseliasan control-plane actions (node adopt/release, tunneled node commands, RBAC role/disable/elevate changes, fleet-key rotation, node-access grant changes).

## Constructor

`NewAuditService(db, logf)` — takes the DB and an optional `logf` diagnostics callback (invoked, never propagated, on a write failure). Wired once in `app.go` and shared across every API that audits an action.

## AuditEntry

The caller-facing shape passed to `Record`: `Action`, `ActorId`, `ActorEmail`, `ActorRole`, `TargetType`, `TargetId`, `Outcome` (defaults to `"success"` when empty), `Detail`, `Metadata` (`map[string]any`, marshaled to JSON), `ClientIp`. The service stamps `CreatedAt` itself.

## Responsibilities

- `Record(ctx, e AuditEntry)` — inserts one `entities.AuditLog` row. **Best-effort**: a write failure is passed to `logf` (if set) but never returned to the caller, so auditing can never block or fail the action it is auditing.
- `List(ctx, limit, offset, action, targetType, targetId)` — returns audit entries newest-first (`Id DESC`), optionally narrowed by any combination of `action` / `targetType` / `targetId` (empty string = no filter on that field). `limit` defaults to 100 and is capped at 500.

## Notes

- Only `Record` and `List` are exposed — no update or delete — so the trail is tamper-evident; see `entities/audit_log.go.md`.
- `List` treats "no rows matched" the same as an empty result rather than an error (`isNoResultFoundErr`).
- `apis/audit.go` calls `List` for the superadmin-gated read API; `apis/nodes.go`, `apis/rbac_admin.go`, `apis/node_access_api.go`, and `apis/node_proxy.go` call `Record` from their sensitive-action handlers.
