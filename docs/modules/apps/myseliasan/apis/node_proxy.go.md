# Module: apps/myseliasan/apis/node_proxy.go

## Purpose

Implements the commander's reverse-tunnel proxy: forwards any HTTP method sent to `/api/nodes/{id}/proxy/<node-path>` over the node's live control channel to the node's own API router.

## Route

```
ANY /api/nodes/{id}/proxy/{node-path...}
```

Registered on its own `/nodes` subrouter, matched after `NewNodesApi`'s specific routes so the path catch-all only fires for proxy requests.

## Request Flow

1. Extract `nodeID` from the path and strip the `/api/nodes/{id}/proxy` prefix to derive `nodePath` (e.g. `/api/settings/users`). Query string is preserved.
2. Check `INodeAccessService.Resolve(callerRoleId, nodeID)`:
   - Empty `Role()` → 403.
   - `"admin"` → node request runs as admin on the node.
   - `"viewer"` → node request runs as viewer on the node.
3. Read request body (capped at 8 MiB).
4. Build `control.Request{Method, Path, Role, Actor, Headers, Body}` and send via `ControlSender.SendRequest`.
5. Return the node's response status + body verbatim. `ErrNodeOffline` → 404.

## Actor Attribution

`operatorIdentity(r)` extracts the calling operator's `email` (preferred), `name`, or numeric ID from JWT claims as the `Actor` string. The node-side dispatcher prefixes it with `"cp:"` for audit attribution of tunneled writes.

## Notes

- `maxProxyBodyBytes = 8 MiB` caps forwarded request bodies.
- The node enforces its own authorization on the re-injected request (the synthetic principal carries the resolved role).
- Only `Content-Type` is forwarded from request headers to the node; other headers are dropped.
