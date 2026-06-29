# Module: apps/myseliasan/apis/nodes.go

## Purpose

Exposes the control-plane node-management HTTP endpoints: fleet-key management, LAN discovery, node adoption, and release.

## Endpoints

### Operator routes (require a myseliasan session)

| Method | Path | Notes |
|---|---|---|
| GET | `/api/nodes` | List all adopted nodes (`ManagedNode` rows). |
| POST | `/api/nodes/scan` | Discover unpaired nodes on the LAN. Body: `{"timeoutMs": N}` (default 4000). Returns `[]DiscoveredNode` (with `adopted` flag). Returns 400 when no fleet key is set. |
| POST | `/api/nodes/adopt` | Adopt a node. Body: `AdoptInput` (`nodeId`, `name`, `ip`, `httpsPort`, `claimCode`). Returns the saved `ManagedNode`. Returns 400 on missing fleet key; 409 when the node rejected adoption (already paired). |
| POST | `/api/nodes/{id}/release` | Release an adopted node. Calls the node's release endpoint (best-effort) then drops the registry row. |
| GET | `/api/nodes/fleet-key` | Return `{"fleetKey": "...", "set": bool}`. |
| POST | `/api/nodes/fleet-key` | Generate (rotate) the fleet key. Returns `{"fleetKey": "...", "set": true}`. |

### Public routes (no session required; carry their own authentication)

| Method | Path | Authentication | Notes |
|---|---|---|---|
| POST | `/api/nodes/self-dropped` | Fleet-key assertion | A node reports it unpaired itself. Body: `{"nodeId","nonce","ts","assertion"}`. The assertion is verified against the fleet key before the node's status is updated to `"self-dropped"`. Returns 401 on bad/missing assertion. |
| POST | `/api/nodes/enroll` | Pairing token | Node-initiated CSR signing. Body: `{"nodeId","token","csr"}`. The token is the one issued to the node at adoption. Returns `{"nodeCert":"<PEM>","caRoot":"<PEM>"}` on success; 403 on unknown node or token mismatch; 451 if the node is revoked. Used for both initial enrollment and cert renewal. |

## Notes

- `NewNodesApi` registers public routes (self-drop, enroll) directly on the top-level router so they are reachable without a session.
- The session-protected group uses `auth.Middleware` + `session.Middleware` (the shared `AccessSessionMidware`). The accessrbac matrix gates operator node operations (viewer role needs GET access to `/api/nodes`; mutations need POST/DELETE; superadmin bypasses).
- Request bodies are capped at 64 KiB and parsed with `DisallowUnknownFields`.
