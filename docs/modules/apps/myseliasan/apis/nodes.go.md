# Module: apps/myseliasan/apis/nodes.go

## Purpose

Exposes the control-plane node-management HTTP endpoints: fleet-key management, LAN discovery, node adoption, and release.

## Endpoints

### Operator routes (require a myseliasan session)

| Method | Path | Notes |
|---|---|---|
| GET | `/api/nodes` | List all adopted nodes (`ManagedNode` rows). |
| GET | `/api/nodes/fleet-status` | Fleet liveness + certificate-health rollup: `{total, online, lost, selfDropped, unknown, certsExpiring, certsExpired, certWarnDays}`. Backs the dashboard's "certs expiring" KPI. |
| POST | `/api/nodes/scan` | Discover unpaired nodes on the LAN. Body: `{"timeoutMs": N}` (default 4000). Returns `[]DiscoveredNode` (with `adopted` flag). Returns 400 when no fleet key is set. |
| POST | `/api/nodes/adopt` | Adopt a node. Body: `AdoptInput` (`nodeId`, `name`, `ip`, `httpsPort`, `claimCode`). Returns the saved `ManagedNode`. Returns 400 on missing fleet key; 409 when the node rejected adoption (already paired). |
| PUT | `/api/nodes/{id}` | Edit an adopted node's operator-facing fields: `{name, description, icon}`. Identity/trust fields are untouched. Returns the updated `ManagedNode`; 400 on an unknown node or bad body. |
| PUT | `/api/nodes/{id}/position` | Set a node's geographic map coordinates from an operator dragging its pin: `{lat, lon, placed}`. Separate from the meta update above so a drag never round-trips name/description/icon. `placed` defaults to `true` when omitted (the common drag case); an explicit `false` unplaces the node (clears it off the map) and coordinates are not range-checked in that case. Otherwise `lat`/`lon` are validated to `[-90,90]`/`[-180,180]`. Returns the updated `ManagedNode`; 400 on an unknown node, out-of-range coordinates, or bad body. |
| GET | `/api/nodes/unrecognized` | List **stranded** nodes: valid fleet-CA cert, but no usable managed record (`ControlServer.Unrecognized()` via the `rejectTracker` seam — see "Stranded node visibility" below). Registered before the `/{id}` routes so its literal path segment wins. Returns `[]` when no `rejectTracker` is wired yet (early in boot) rather than erroring. |
| POST | `/api/nodes/{id}/block` | Revoke a stranded node's certificate (`INodeRegistry.RevokeNode`) so it can no longer enroll or hold a control connection, then drops it from the unrecognized list. The control-plane-side "remove" for a node with no managed record to `release`; the node itself still needs a factory reset to stop dialing entirely (the UI explains this). Audited as `node.block`. |
| POST | `/api/nodes/{id}/forget` | Dismiss a node from the unrecognized list **without** revoking — it reappears if the node dials and is refused again. Audited as `node.forget`. |
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
- `NewNodesApi` now returns the built `*nodesApi` (previously returned nothing) so `app.go` can call `SetRejectTracker` on it after the `ControlServer` is constructed — the control server, which is what actually tracks refused connections, is built later in startup than this API is registered.

## Stranded node visibility (`/unrecognized`, `/block`, `/forget`)

A node that holds a **valid** fleet-CA certificate but has no usable managed record here — typically released on the control plane but never reset on its own side, or a row lost to a DB issue — used to be entirely invisible: the control channel just refused the connection and closed it, over and over, with nothing surfaced beyond a recurring log line. `rejectTracker` (satisfied by `*services.ControlServer`, wired in via `SetRejectTracker` after that server is built) exposes that otherwise-silent refusal:

- `GET /api/nodes/unrecognized` lists them (`services.RejectedNode`: `nodeId`, `reason`, `remoteAddr`, `firstSeen`, `lastSeen`, `count`), letting an operator see a node that keeps dialing and failing.
- `POST /api/nodes/{id}/block` revokes the stranded node's cert (so it can never enroll or connect again) and drops it from the list — the control-plane-side "remove" for a node with no row to `release`.
- `POST /api/nodes/{id}/forget` just dismisses the entry (no revocation); it comes back if the node dials again and is still refused.
- `a.rejects == nil` (before `SetRejectTracker` runs, or in a test that never wires it) makes `listUnrecognized` return an empty list rather than a 500 — a nil tracker is a normal boot-order state, not an error.
- See `services/control_server.go.md` for how a rejection is recorded and deduped, and `services/node_registry.go.md`'s `RevokeNode`/`AcceptControlConn` for the registry side of blocking.

## Audit trail

`NewNodesApi(router, auth, session, registry, audit, logf)` now also takes `services.IAuditService` and a `logf func(string, ...any)` (server-log sink for detail an operator-facing error message omits, e.g. the raw DB error behind a failed adopt; may be nil). The following actions are recorded (best-effort; never blocks the request) via `services.AuditEntry` with `TargetType: "node"` (or `"fleet"` for the key rotation):

| Action | When | Target | Notes |
|---|---|---|---|
| `node.adopt` | `adopt` succeeds or fails | node id | Failure entries carry `outcome: "error"`; success entries include `{ip, name}` in `Metadata`. A failure that reached `ErrAdoptPersist` (node paired, but the record could not be saved) also logs the raw error via `logf` and returns an actionable message explaining the pairing was rolled back — see `services/node_registry.go.md`. |
| `node.release` | `release` succeeds or fails | node id | Success detail notes the certificate was revoked. |
| `node.block` | `blockNode` succeeds or fails | node id | Revokes a stranded node's cert; success detail notes it. |
| `node.forget` | `forgetNode` | node id | Dismissal only — no revocation. |
| `fleet.key_rotate` | `generateFleetKey` succeeds or fails | fleet (no target id) | **The rotated key value is never recorded** — only that a rotation happened. |
| `node.self_dropped` | a node calls `POST /api/nodes/self-dropped` | node id | Node-initiated: no operator session, so `ActorEmail` is set to `"node:<id>"` and `ActorId`/`ActorRole` are left zero. |

`recordNodeAction` / `recordFleetAction` are the two small wrappers that fill in the actor (via `auditActor(r)`) and client IP (via `clientIP(r)`) before calling `audit.Record`.
