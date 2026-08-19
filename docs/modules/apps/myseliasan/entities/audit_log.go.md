# Module: apps/myseliasan/entities/audit_log.go

## Purpose

`AuditLog` is now a type alias onto `domain/shared/audit.AuditLog`
(`docs/modules/domain/shared/audit/service.go.md`), the shared append-only audit trail
adopted by myidsan, myseliasan and mymatasan. Previously an independent copy of myidsan's
entity that had already begun to drift from it (myseliasan's own lacked `UserAgent` and any
retention path).

Stored in table `audit_log`, unaffected by the move — the schema bootstrapper derives the
table name from the struct name, which is still `AuditLog`.

## Fields

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `Action` | Verb.noun of what happened, e.g. `node.adopt`, `node.release`, `node.command`, `rbac.set_role`, `fleet.key_rotate` — see `services/audit.go.md`. Indexed. |
| `ActorId` | The control-plane user who performed the action (`0` for a node/system-initiated action such as a self-drop). |
| `ActorEmail` | Human-readable attribution captured at action time (email, else display name, else `"node:<id>"` for node-initiated events). Now indexed (`idx:"actor"`), gained from the shared entity. |
| `ActorRole` | The actor's role id at the time of the action. |
| `TargetType` | What was acted on: `"node"`, `"user"`, `"fleet"`, `"node-access"`. |
| `TargetId` | Identifies the target within its type (node id, user id, etc.). Indexed. |
| `Outcome` | `"success"`, `"denied"`, or `"error"`. |
| `Detail` | Short human-readable summary of the action. |
| `Metadata` | Optional JSON blob carrying structured context (before/after values, request path, extra fields). |
| `ClientIp` | Source address of the operator's request (empty for node-initiated events). |
| `UserAgent` | **New**, gained from the shared entity, applied additively by the auto-migrator. Previously unique to myidsan; myseliasan's own copy lacked it. |
| `CreatedAt` | Unix timestamp. Indexed. |

## Notes

- **Append-only by design**: there is no update or delete path, so the trail is tamper-evident for incident review. Only `services.IAuditService.Record` ever inserts a row. Age-based retention now exists via the shared package (`domain/shared/audit/retention.go`, `PurgeOlderThan`), reachable only from configuration and archive-first — see `docs/modules/domain/shared/audit/service.go.md`. myseliasan had no retention path before this move.
- **Distinct from `api_log`**: `api_log` is a per-request HTTP access log subject to retention-based deletion and carries no action semantics; `audit_log` records specific sensitive operator/system actions.
- Registered in `apps/myseliasan/app/app.go`'s `Entities()` so bootstrap creates the `audit_log` table alongside the rest of myseliasan's schema; the additive `user_agent` column and `ActorEmail` index are reconciled onto existing installs by the bootstrap drift check.
