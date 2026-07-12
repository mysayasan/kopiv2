# Module: apps/myseliasan/apis/node_access_api.go

## Purpose

HTTP endpoints for managing per-role node **device** access grants — distinct from the `/api/nodes/*` path matrix, which gates myseliasan's own endpoints. A grant's level is one of two, mirroring mymatasan's own local levels: `viewer` (`canRead`, read-only) or `admin` (`canRead`+`canWrite`, drives the node as its admin). The node's owning role and RBAC superadmins may view or change grants; a `?roleId=` lens lets superadmins query a role's grants across all nodes for the central RBAC node-access matrix.

## Endpoints

All routes require a myseliasan session (`auth.Middleware`).

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/nodes/access?nodeId=ID` | List all `NodeAccessGrant` rows for the given node. Caller must be a superadmin or the node's owner. |
| `GET` | `/api/nodes/access?roleId=ID` | List all grants a role holds across every node. **Superadmin only.** Powers the central RBAC node-access matrix. |
| `POST` | `/api/nodes/access` | Upsert a grant `{roleId, nodeId, canRead, canWrite}`. Caller must be a superadmin or the node's owner. |
| `DELETE` | `/api/nodes/access/{id}` | Remove a grant. Caller must be a superadmin or the owner of the grant's node. |

## Authorization

- `requireManager` admits the caller if `isSuperadmin(r)` (live role from user store via `AccessSessionMidware`) or `isOwner(r, nodeID)` (live principal's role via `CurrentPrincipal`). Non-managers receive 403. The `?roleId=` lens additionally calls `requireSuperadmin`.
- Authorization resolves the **live role** from the user store on every request (not the token's baked roleId), so a just-demoted account immediately loses node-management access without a re-login.
- The owner's role is the `ManagedNode.OwnerRoleId` recorded at adoption time.

## Constructor

`NewNodeAccessApi(router, auth, access, session, audit)` — now also takes `services.IAuditService`. The `*AccessSessionMidware` lets live-role checks be performed inside the handlers.

## Notes

- Registered on its own `/nodes/access` subrouter; mux matches these specific paths before the proxy catch-all.
- `CanWrite=true` forces `CanRead=true` at the service layer.

## Audit trail

`upsert` and `delete` each record an entry (`TargetType: "node-access"`, `TargetId` = the grant's node id) via `recordGrantAction`, best-effort:

| Action | Handler | Detail / Metadata |
|---|---|---|
| `node_access.set` | `upsert` | Detail notes the granted role/node and read/write flags; `Metadata: {roleId, canRead, canWrite}`. |
| `node_access.revoke` | `delete` | Detail notes the revoked role/node; `Metadata: {roleId, grantId}`. |
