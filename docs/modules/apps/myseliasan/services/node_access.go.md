# Module: apps/myseliasan/services/node_access.go

## Purpose

Implements `INodeAccessService`, the service layer for per-role node access grants on the myseliasan control plane.

## Constructor

`NewNodeAccessService(db, roles)` — takes the DB and the shared `IAccessRoleService`. The roles service lets the access service grant superadmin roles implicit full access to every node without an explicit grant.

## Responsibilities

- `Resolve(ctx, roleId, nodeId)` — returns `NodeAccess{CanRead, CanWrite}` for the (role, node) pair. A **superadmin role** now receives implicit full access (`CanRead=true, CanWrite=true`) without any explicit grant or ownership — mirrors the permission model on mymatasan. Otherwise: the node's owning role (`ManagedNode.OwnerRoleId`) gets full access; other roles need an explicit `NodeAccessGrant` row.
- `OwnsNode(ctx, roleId, nodeId)` — reports whether a role is the node's owner.
- `ListForNode(ctx, nodeId)` — returns all grant rows for a node.
- `ListForRole(ctx, roleId)` — returns all grant rows a role holds across every node. Used by the central RBAC node-access matrix where a superadmin assigns per-node access to any role.
- `Set(ctx, NodeAccessGrant)` — upsert by `(roleId, nodeId)`. Enforces `CanRead=true` when `CanWrite=true`.
- `Delete(ctx, id)` — removes a grant.
- `GrantById(ctx, id)` — fetches a grant by ID (used by the delete handler to check the node before deleting).

## NodeAccess

`NodeAccess{CanRead, CanWrite}` is the resolved access for a `(role, node)` pair. The proxy API derives an effective role string from it: `CanWrite=true → "admin"`, `CanRead=true → "viewer"`, neither → `""` (forbidden). This string is forwarded in the `control.Request.Role` field so the node's dispatcher synthesizes the correct local principal.

## Notes

- Superadmin short-circuit is checked first via `isSuperadminRole` (nil-safe; returns false in unit tests where no roles service is wired).
- Owner check reads `ManagedNode.OwnerRoleId`; no ownership and no superadmin means checking the `node_access_grant` table.
- The proxy API (`node_proxy.go`), media API (`node_media.go`), and access-grant API (`node_access_api.go`) all resolve the live role from the user store (via `AccessSessionMidware.CurrentPrincipal`) before calling `Resolve`.
