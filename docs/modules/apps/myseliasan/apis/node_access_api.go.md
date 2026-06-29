# Module: apps/myseliasan/apis/node_access_api.go

## Purpose

HTTP endpoints for managing per-role node access grants. Only the node's owning role may view or change its grants.

## Endpoints

All routes require a myseliasan session (`auth.Middleware`).

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/nodes/access?nodeId=ID` | List all `NodeAccessGrant` rows for the given node. Caller's role must own the node. |
| `POST` | `/api/nodes/access` | Upsert a grant `{roleId, nodeId, canRead, canWrite}`. Caller's role must own the node. |
| `DELETE` | `/api/nodes/access/{id}` | Remove a grant. Caller's role must own the grant's node. |

## Authorization

- `requireOwner` checks `INodeAccessService.OwnsNode(callerRoleId, nodeID)`. Non-owners receive 403.
- The owner's role is the `ManagedNode.OwnerRoleId` recorded at adoption time.

## Notes

- Registered on its own `/nodes/access` subrouter; mux matches these specific paths before the proxy catch-all.
- `CanWrite=true` forces `CanRead=true` at the service layer.
