# Module: apps/myseliasan/services/node_access.go

## Purpose

Implements `INodeAccessService`, the service layer for per-role node access grants on the myseliasan control plane.

## Responsibilities

- `Resolve(ctx, roleId, nodeId)` — returns an `AccessLevel` (the derived read/write/none for the role on that node). The node's owning role (`ManagedNode.OwnerRoleId`) gets full access without a grant; other roles need an explicit `NodeAccessGrant` row.
- `OwnsNode(ctx, roleId, nodeId)` — reports whether a role is the node's owner.
- `ListForNode(ctx, nodeId)` — returns all grant rows for a node.
- `Set(ctx, NodeAccessGrant)` — upsert by `(roleId, nodeId)`. Enforces `CanRead=true` when `CanWrite=true`.
- `Delete(ctx, id)` — removes a grant.
- `GrantById(ctx, id)` — fetches a grant by ID (used by the delete handler to check the node before deleting).

## AccessLevel

`AccessLevel` is the resolved access for a `(role, node)` pair. Its `Role()` method returns `"admin"` (read+write), `"viewer"` (read-only), or `""` (no access). This string is forwarded in the `control.Request.Role` field so the node's dispatcher synthesizes the correct local principal.

## Notes

- Owner check reads `ManagedNode.OwnerRoleId`; no ownership means checking the `node_access_grant` table.
- The proxy API (`node_proxy.go`) calls `Resolve` on every tunneled request to enforce access in real time.
