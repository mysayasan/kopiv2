# Module: apps/mymatasan/apis/manual.go

## Purpose

Registers mymatasan's built-in user manual (`apps/mymatasan/manual.Library`,
`apps/mymatasan/manual/manual.go.md`) over the shared handler set
(`domain/shared/apis.ManualHandlers`, `domain/shared/apis/manual.go.md`).

## Responsibilities

- `NewManualApi(router *mux.Router)` mounts under `/manual`:
  - `GET /manual` — article index (`List`).
  - `GET /manual/bundle` — the whole book, bodies included (`Bundle`).
  - `GET /manual/assets/{name}` — figures (`Asset`).
  - `GET /manual/{slug}` — one article (`Get`); registered **last** so `bundle` and
    `assets` are matched by their own routes first.
- Called from `apps/mymatasan/app/wire_routes.go` (`app/wire_routes.go.md`) on the
  **public** router, before the auth-protected subrouter.

## Notes

- **Mounted on the public router deliberately, not an oversight.** Two of the places a
  reader most needs the manual — the sign-in screen and the first-run wizard's earliest
  steps — are places they either cannot authenticate yet or are stuck trying to. What that
  exposes is shipped, read-only documentation compiled into the binary: no runtime state,
  no per-user data, no configuration values, nothing an operator has typed — the same
  posture as the already-unauthenticated sign-in page that names the product.
- **Deliberately kept off the RBAC matrix.** Primarily because the sign-in screen and the
  first-run wizard — where the manual is most needed — have no session to check a matrix
  against, regardless of how the matrix is seeded. There was also a second, narrower
  benefit at the time this was written: `EnsureApplianceRoles` skipped seeding a role
  wholesale once it had any permission rows, so a rule gating the manual would have reached
  only brand-new installs. That gap was closed by the additive-backfill fix in
  `domain/shared/services/appliance_rbac.go` (`appliance_rbac.go.md`) — a catalog rule now
  reaches an already-seeded role on its next boot — so this is no longer a reason to keep
  the manual public, but the pre-auth reachability requirement above still is.
- Note: the code comment on `NewManualApi` still states the pre-backfill reasoning
  ("`EnsureApplianceRoles` seeds a role's permissions only when that role has none"); it
  describes behavior that changed under it and is worth a follow-up comment update, though
  the routing decision itself (public, off the matrix) is unaffected.
- Endpoint metadata seed row: `{Title: "User Manual", Path: "/api/manual", AccessTier:
  apiaccessenums.Public}` in `apps/mymatasan/app/app.go` (`app/app.go.md`).
