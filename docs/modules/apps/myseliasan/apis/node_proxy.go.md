# Module: apps/myseliasan/apis/node_proxy.go

## Purpose

Implements the commander's reverse-tunnel proxy: forwards any HTTP method sent to `/api/nodes/{id}/proxy/<node-path>` over the node's live control channel to the node's own API router.

## Route

```
ANY /api/nodes/{id}/proxy/{node-path...}
```

Registered on its own `/nodes` subrouter, matched after `NewNodesApi`'s specific routes and after `NewRecordingStreamApi`'s `/nodes/{id}/recording-stream/{segId}` route (see `recording_stream.go.md`), so the path catch-all only fires for proxy requests.

## Constructor

`NewNodeProxyApi(router, auth, sender, access, session, audit)` — takes the `*AccessSessionMidware` so the proxy resolves the caller's **live role** from the user store on every tunneled request (a just-demoted operator immediately loses node access without a re-login), and `services.IAuditService` to audit mutating tunneled commands.

## Request Flow

1. Extract `nodeID` from the path and strip the `/api/nodes/{id}/proxy` prefix to derive `nodePath` (e.g. `/api/settings/users`). Query string is preserved.
2. Resolve the caller's live `roleId` via `AccessSessionMidware.CurrentPrincipal` (falls back to the token's baked `roleId` if the session middleware is not wired).
3. Check `INodeAccessService.Resolve(callerRoleId, nodeID)`:
   - No access → 403.
   - Write access → node request runs as admin on the node.
   - Read-only → node request runs as viewer on the node.
3. Read request body (capped at 8 MiB).
4. Build `control.Request{Method, Path, Role, Actor, Headers, Body}` and send via `ControlSender.SendRequest`.
5. Return the node's response status + body verbatim. `ErrNodeOffline` (never connected) or `ErrNodeDisconnected` (control channel dropped mid-command — see `control_server.go.md`) → 404 `"node is not connected"`. A disconnect mid-command fails fast instead of hanging to `controlRequestTimeout`; the caller cannot tell from the 404 alone whether a non-idempotent write had already applied on the node before the drop.

## Actor Attribution

`operatorIdentity(r)` extracts the calling operator's `email` (preferred), `name`, or numeric ID from JWT claims as the `Actor` string. The node-side dispatcher prefixes it with `"cp:"` for audit attribution of tunneled writes.

## Notes

- `maxProxyBodyBytes = 8 MiB` caps forwarded request bodies.
- The node enforces its own authorization on the re-injected request (the synthetic principal carries the resolved role).
- Only `Content-Type` is forwarded from request headers to the node; other headers are dropped.

## Audit trail

`recordCommand` audits every **mutating** tunneled command (`POST`/`PUT`/`PATCH`/`DELETE`) as `Action: "node.command"`, `TargetType: "node"`, `TargetId` = the node id — this is the single choke point through which remote wipe/factory-reset/settings-writes reach a node, so it is the natural place to catch them all. `GET`/`HEAD`/`OPTIONS` (read-only tunneled traffic) are **not** audited, to avoid drowning the trail in routine page loads. `Metadata` carries `{method, path, nodeStatus}` (path has its query string stripped for a stable detail string). `outcomeForStatus` maps the node's HTTP response into the audit outcome: `2xx` → `"success"`, `401`/`403` → `"denied"`, anything else (including a send failure, recorded with `status 0`) → `"error"`.
