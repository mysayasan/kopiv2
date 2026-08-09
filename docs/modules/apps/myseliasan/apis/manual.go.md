# Module: apps/myseliasan/apis/manual.go

## Purpose

Registers myseliasan's built-in user manual (`apps/myseliasan/manual.Library`,
`apps/myseliasan/manual/manual.go.md`) over the shared handler set
(`domain/shared/apis.ManualHandlers`, `domain/shared/apis/manual.go.md`) — the same
pattern `apps/mymatasan/apis/manual.go` (`apps/mymatasan/apis/manual.go.md`) established,
adapted to myseliasan's own route prefix.

## Responsibilities

- `NewManualApi(router *mux.Router)` mounts under `/manual` (reached as `/api/manual`
  once `router` itself is the API subrouter):
  - `GET /manual` — article index (`List`).
  - `GET /manual/bundle` — the whole book, bodies included (`Bundle`).
  - `GET /manual/assets/{name}` — figures (`Asset`).
  - `GET /manual/{slug}` — one article (`Get`); registered **last** so `bundle` and
    `assets` are matched by their own routes first.
- Called from `apps/myseliasan/app/app.go`'s `RegisterAppRoutes` (`app/app.go.md`) as
  `apis.NewManualApi(api)`, on the **bare** `api` router — the same parameter every other
  auth-protected myseliasan route also registers against, but called before any
  `api.Use(...)` auth middleware is attached, so the manual alone stays reachable
  pre-session.

## Notes

- **Mounted on the bare router deliberately, not an oversight.** myseliasan's own doc
  comment on `NewManualApi` spells out why: the sign-in screen is one of the two places a
  reader most needs the manual (the first-run wizard is the other), and both are places
  they cannot authenticate yet. What that exposes is shipped, read-only documentation
  compiled into the binary: no runtime state, no per-user data, nothing an operator has
  typed. The shared rate limiter still applies.
- **Deliberately kept off the RBAC permission matrix**, for the same reason as
  mymatasan's: the sign-in screen and the first-run wizard — where the manual is most
  needed — have no session to check a matrix against.
- Endpoint metadata seed row: `{Title: "User Manual", Description: "the built-in manual;
  public so help works on the sign-in screen and in the first-run wizard", Path:
  "/api/manual", AccessTier: apiaccessenums.Public}` in `apps/myseliasan/app/app.go`
  (`app/app.go.md`).
