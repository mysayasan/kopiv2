# Module: apps/myseliasan/entities/node_access_grant.go

## Purpose

Entity for per-role, per-node access grants on the myseliasan control plane. Stored in table `node_access_grant`.

## Fields

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `RoleId` | The myseliasan role being granted access. |
| `NodeId` | The target `ManagedNode.NodeId`. |
| `CanRead` | Lowest rung. Role may read (view) this node via the proxy tunnel — `"viewer"` on the node. |
| `CanOperate` | Middle rung. Role may also review footage, acknowledge alerts, PTZ, talk-back — `"operator"` on the node. |
| `CanWrite` | Top rung. Role may do everything, including deleting footage — `"admin"` on the node. |
| `CreatedBy / UpdatedBy / CreatedAt / UpdatedAt` | Audit fields. |

## Notes

- The three flags are an **escalation ladder**, not independent bits: `CanWrite` implies `CanOperate` implies `CanRead`, enforced on save (`services/node_access.go`'s `normalizeAccess`). A grant is really a single choice of level, expressed as flags for backward compatibility with rows already in the field.
- `CanOperate` is the rung added in phase R part (b) (2026-07-14). Existing rows have `canOperate=false` — that is correct, since they predate the rung, and nothing silently gains a capability on upgrade.
- A node's owning role (`ManagedNode.OwnerRoleId`) has full access without an explicit grant; only non-owner roles need a grant row.
- A role with no ownership and no grant for a node is denied by the proxy handler.
- `CanWrite` maps to `admin` on the node; `CanOperate` (without `CanWrite`) maps to `operator`; `CanRead` only maps to `viewer`; none of the three means forbidden.
