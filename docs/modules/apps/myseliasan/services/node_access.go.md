# Module: apps/myseliasan/services/node_access.go

## Purpose

Implements `INodeAccessService`, the service layer for per-role node access grants on the myseliasan control plane.

## Constructor

`NewNodeAccessService(db, roles)` — takes the DB and the shared `IAccessRoleService`. The roles service lets the access service grant superadmin roles implicit full access to every node without an explicit grant.

## Responsibilities

- `Resolve(ctx, roleId, nodeId)` — returns a normalised `NodeAccess{CanRead, CanOperate, CanWrite}` for the (role, node) pair. A **superadmin role** receives implicit full access (`CanWrite=true`, normalised to all three) without any explicit grant or ownership — mirrors the permission model on mymatasan. Otherwise: the node's owning role (`ManagedNode.OwnerRoleId`) gets full access; other roles need an explicit `NodeAccessGrant` row, which is re-normalised on read so a stale or hand-edited row cannot express an incoherent ladder (e.g. write without read).
- `OwnsNode(ctx, roleId, nodeId)` — reports whether a role is the node's owner.
- `ListForNode(ctx, nodeId)` — returns all grant rows for a node.
- `ListForRole(ctx, roleId)` — returns all grant rows a role holds across every node. Used by the central RBAC node-access matrix where a superadmin assigns per-node access to any role.
- `Set(ctx, NodeAccessGrant)` — upsert by `(roleId, nodeId)`. Runs the grant through `normalizeAccess` before saving, so `CanWrite` implies `CanOperate` implies `CanRead` regardless of what the caller sent.
- `Delete(ctx, id)` — removes a grant.
- `GrantById(ctx, id)` — fetches a grant by ID (used by the delete handler to check the node before deleting).

## NodeAccess

`NodeAccess{CanRead, CanOperate, CanWrite}` is the resolved access for a `(role, node)` pair — an **escalation ladder** where each rung implies the ones below it. `normalizeAccess` is the single place that enforces the ladder (`CanWrite` → also `CanOperate`; `CanOperate` → also `CanRead`); both `Resolve` and `Set` route through it.

`Role()` derives the effective role NAME sent over the tunnel: `CanWrite=true → "admin"`, else `CanOperate=true → "operator"`, else `CanRead=true → "viewer"`, else `""` (forbidden). This string is forwarded in the `control.Request.Role` field. The node does not take it as a permission — it takes it as an identity claim, resolves the name against its own roles, and evaluates its own matrix; a compromised control plane cannot assert capabilities the node never granted.

`CanOperate` is the middle rung (added phase R part (b), 2026-07-14): without it a control-plane user was either a viewer or an admin at the node, so a fleet operator who should review footage but not delete it had to be given the power to delete it.

## Notes

- Superadmin short-circuit is checked first via `isSuperadminRole` (nil-safe; returns false in unit tests where no roles service is wired).
- Owner check reads `ManagedNode.OwnerRoleId`; no ownership and no superadmin means checking the `node_access_grant` table.
- The proxy API (`node_proxy.go`), media API (`node_media.go`), and access-grant API (`node_access_api.go`) all resolve the live role from the user store (via `AccessSessionMidware.CurrentPrincipal`) before calling `Resolve`.
- Existing grant rows have `CanOperate=false` by construction, so nothing silently gains a capability on upgrade — covered by `TestNodeAccess_ExistingGrantsAreUnchangedOnUpgrade`.
