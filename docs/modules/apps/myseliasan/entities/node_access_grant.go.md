# Module: apps/myseliasan/entities/node_access_grant.go

## Purpose

Entity for per-role, per-node access grants on the myseliasan control plane. Stored in table `node_access_grant`.

## Fields

| Field | Notes |
|---|---|
| `Id` | Primary key. |
| `RoleId` | The myseliasan role being granted access. |
| `NodeId` | The target `ManagedNode.NodeId`. |
| `CanRead` | Role may read (view) this node via the proxy tunnel (viewer on the node). |
| `CanWrite` | Role may also write (admin) on the node. Write implies read; enforced on save. |
| `CreatedBy / UpdatedBy / CreatedAt / UpdatedAt` | Audit fields. |

## Notes

- A node's owning role (`ManagedNode.OwnerRoleId`) has full access without an explicit grant; only non-owner roles need a grant row.
- A role with no ownership and no grant for a node is denied by the proxy handler.
- `CanRead + CanWrite` maps to `admin` on the node; `CanRead` only maps to `viewer`; neither means forbidden.
