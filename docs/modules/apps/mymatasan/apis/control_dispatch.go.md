# Module: apps/mymatasan/apis/control_dispatch.go

## Purpose

Builds the node-side handler for tunneled parent→node commands. Re-injects incoming `control.Request` frames into the node's own `/api` subrouter as synthetic HTTP requests, so the node's existing authorization stack (`NewLocalBasicAuth` + `NewRequireAdminForWrites`) enforces viewer/admin without any new auth code.

## Key Function: NewControlDispatcher

```go
func NewControlDispatcher(router http.Handler) services.ControlDispatcher
```

Returns a `ControlDispatcher` (a function `func(ctx, control.Request) control.Response`) that:

1. Constructs a synthetic `*http.Request` from the frame's `Method`, `Path`, `Headers`, and `Body`.
2. Injects an `AuthenticatedUser` principal into the request context (keyed by `localAuthContextKey{}`):
   - `Username` = `"cp:" + req.Actor` (labeled for audit attribution).
   - `IsAdmin` = `strings.EqualFold(req.Role, "admin")`.
3. Dispatches the request through `router.ServeHTTP` using `httptest.NewRecorder`.
4. Returns a `control.Response` with the recorded status, **all** response headers, and body.

## Notes

- The router passed in is the app's `/api` subrouter, so `req.Path` must include the `/api` prefix (e.g. `/api/settings/users`).
- The parent sends `req.Role = "admin"` or `"viewer"` based on the access grant; the node's write-authorization middleware gates mutations via `IsAdmin`.
- Invalid method or body returns `400`; all other errors are returned verbatim from the node's handlers.
- All recorded response headers are copied into `control.Response.Headers` (previously only `Content-Type` was forwarded). This is what lets `Content-Range`, `Accept-Ranges`, and `Content-Length` reach `myseliasan`'s `/api/nodes/{id}/recording-stream/{segId}` intact, so tunneled recording playback can serve `206 Partial Content` and seek.
