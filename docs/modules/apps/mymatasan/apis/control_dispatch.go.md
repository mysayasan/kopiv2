# Module: apps/mymatasan/apis/control_dispatch.go

## Purpose

Builds the node-side handler for tunneled parent→node commands. Re-injects incoming `control.Request` frames into the node's own `/api` subrouter as synthetic HTTP requests, so the node's existing authorization stack (`NewLocalBasicAuth` + `NewRequireRolePermission`) decides them exactly like a locally-authenticated request.

**The parent asserts a ROLE NAME; the node resolves it against its OWN roles and evaluates
its OWN permission matrix.** This direction is deliberate and is the security point of this
file: it does NOT carry a permission set over the wire. If the parent asserted permissions
directly, the node would be trusting the control plane to say who may delete its footage —
and a compromised or buggy parent could assert anything. The node owns the data; the node's
policy governs. All the parent gets to say is "this request is on behalf of an operator",
and the node decides what an operator may do here.

## Key Function: NewControlDispatcher

```go
func NewControlDispatcher(router http.Handler, roles sharedservices.IAccessRoleService) services.ControlDispatcher
```

Returns a `ControlDispatcher` (a function `func(ctx, control.Request) control.Response`) that:

1. Constructs a synthetic `*http.Request` from the frame's `Method`, `Path`, `Headers`, and `Body`.
2. Resolves the asserted role: `normalizeControlRole(req.Role)` maps the wire vocabulary onto
   the node's own role names — `"admin"` is accepted as a wire alias for `services.RoleAdmin`
   (the shared `superadmin` role), so an already-deployed control plane (which only ever sent
   `{admin, viewer}`) keeps working across a mixed-version fleet. The resolved name is looked
   up via `roles.GetByName`; a lookup error returns `503 authorization is unavailable`. An
   **unknown role name resolves to no role**, and a principal with no role is denied
   everything by the matrix (fail closed).
3. Injects an `AuthenticatedUser` principal into the request context via `withLocalUser` (a
   thin wrapper over `sharedapis.WithLocalUser` — the context key itself is unexported to
   `domain/shared/apis`, so no network client can forge a principal):
   - `Username` = `"cp:" + req.Actor` (labeled for audit attribution).
   - `RoleId` / `IsAdmin` = the resolved role's id / `IsSuperadmin` flag (zero value when the
     role could not be resolved).
4. Dispatches the request through `router.ServeHTTP` using `httptest.NewRecorder`.
5. Returns a `control.Response` with the recorded status, **all** response headers, and body.

## Notes

- The router passed in is the app's `/api` subrouter, so `req.Path` must include the `/api` prefix (e.g. `/api/settings/users`).
- The wire vocabulary widened from `{admin, viewer}` to `{admin, operator, viewer}` — the JSON frames have no strict decoding, so an old node simply resolves a role name it doesn't recognize to "no role" (denied) rather than failing to parse.
- Invalid method or body returns `400`; all other errors are returned verbatim from the node's handlers.
- All recorded response headers are copied into `control.Response.Headers` (previously only `Content-Type` was forwarded). This is what lets `Content-Range`, `Accept-Ranges`, and `Content-Length` reach `myseliasan`'s `/api/nodes/{id}/recording-stream/{segId}` intact, so tunneled recording playback can serve `206 Partial Content` and seek.
- Known follow-up: myseliasan's `NodeAccessGrant` today has only `CanRead`/`CanWrite` bools and needs a third level so a control-plane operator maps onto the node's `operator` role rather than being forced into `admin` or `viewer`. See `docs/MYMATASAN_TIER2_PLAN.md` (Phase R).
