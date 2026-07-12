# Module: apps/myseliasan/entities/audit_log.go

## Purpose

Entity for the immutable audit trail of sensitive control-plane actions on myseliasan. Stored in table `audit_log`.

## Fields

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `Action` | Verb.noun of what happened, e.g. `node.adopt`, `node.release`, `node.command`, `rbac.set_role`, `fleet.key_rotate`. Indexed. |
| `ActorId` | The control-plane user who performed the action (`0` for a node/system-initiated action such as a self-drop). |
| `ActorEmail` | Human-readable attribution captured at action time (email, else display name, else `"node:<id>"` for node-initiated events). |
| `ActorRole` | The actor's role id at the time of the action. |
| `TargetType` | What was acted on: `"node"`, `"user"`, `"fleet"`, `"node-access"`. |
| `TargetId` | Identifies the target within its type (node id, user id, etc.). Indexed. |
| `Outcome` | `"success"`, `"denied"`, or `"error"`. |
| `Detail` | Short human-readable summary of the action. |
| `Metadata` | Optional JSON blob carrying structured context (before/after values, request path, extra fields). |
| `ClientIp` | Source address of the operator's request (empty for node-initiated events). |
| `CreatedAt` | Unix timestamp. Indexed. |

## Notes

- **Append-only by design**: there is no update or delete path and no retention cleanup, so the trail is tamper-evident for incident review. Only `services.IAuditService.Record` ever inserts a row.
- **Distinct from `api_log`**: `api_log` is a per-request HTTP access log subject to retention-based deletion and carries no action semantics; `audit_log` records specific sensitive operator/system actions and is never purged.
- Registered in `apps/myseliasan/app/app.go`'s `Entities()` so bootstrap creates the `audit_log` table alongside the rest of myseliasan's schema.
