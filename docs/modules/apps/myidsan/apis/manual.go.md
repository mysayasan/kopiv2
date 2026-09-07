# Module: apps/myidsan/apis/manual.go

## Purpose

Registers myidsan's built-in user manual (`apps/myidsan/manual.Library`,
`apps/myidsan/manual/manual.go.md`) over the shared handler set
(`domain/shared/apis.ManualHandlers`, `domain/shared/apis/manual.go.md`) — the same
pattern `apps/mymatasan/apis/manual.go` and `apps/myseliasan/apis/manual.go` established,
adapted to myidsan's own route prefix. This is myidsan's first manual; it shipped none
before.

## Responsibilities

- `NewManualApi(router *mux.Router)` mounts under `/manual` (reached as `/api/manual`
  once `router` itself is the API subrouter):
  - `GET /manual` — article index (`List`).
  - `GET /manual/bundle` — the whole book, bodies included (`Bundle`).
  - `GET /manual/search` — ranked BM25 search over the manual, `?q=&lang=&limit=` (`Search`).
  - `GET /manual/assets/{name}` — figures (`Asset`).
  - `GET /manual/{slug}` — one article (`Get`); registered **last** so `bundle` and
    `assets` are matched by their own routes first.
- Called from `apps/myidsan/app/app.go`'s `RegisterAppRoutes` (`app/app.go.md`) as
  `apis.NewManualApi(api)`, right after `apis.NewSystemApi`, on the bare `api` router —
  before any `api.Use(...)` auth middleware is attached, so the manual alone stays
  reachable pre-session.

## Notes

- **Mounted on the bare router with no auth middleware, deliberately.** The doc comment on
  `NewManualApi` spells out why: myidsan is the server nobody can sign in to when it is
  misconfigured, and every question its sign-in screen raises — where the bootstrap
  password is, why an account has no role, why enrolment is being demanded — is asked by
  somebody who is not authenticated yet. A manual behind the session cookie would be
  missing at exactly the moment it is wanted. What that exposes is shipped documentation
  compiled into the binary: no runtime state, no per-user data, nothing an operator has
  typed. The shared rate limiter still applies.
- **Deliberately kept off the RBAC permission matrix**, for the same reason as
  mymatasan's and myseliasan's: the sign-in screen and the first-run wizard — where the
  manual is most needed — have no session to check a matrix against.
- Endpoint metadata seed row: `{Title: "User Manual", Description: "the built-in manual;
  public so help works on the sign-in screen and in the first-run wizard", Path:
  "/api/manual", AccessTier: apiaccessenums.Public}` in `apps/myidsan/app/app.go`
  (`app/app.go.md`), seeded alongside the other pre-session `Public` rows (`/api/auth`,
  `/api/callback`, `/api/file-storage/download`).
