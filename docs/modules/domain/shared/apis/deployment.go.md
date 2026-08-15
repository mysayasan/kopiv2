# Module: domain/shared/apis/deployment.go

## Purpose

Shared HTTP handler set for the deployment-mode question and its readiness checklist, built on
top of `sharedservices.IDeploymentModeService`/`Preflight` (`domain/shared/services/deployment.go.md`).
As with the setup/factory-reset handlers, route registration stays per-app (they differ in how an
admin write is gated, and Tier B apps expose no write route at all), but the payloads are identical
so one shared SPA component (`frontend/shared/src/Deployment.js`) works everywhere.

## Behavior

- `DeploymentHandlers{mode, env}` / `NewDeploymentHandlers(mode, env)` — `mode` may be `nil` for an
  appliance app with no runtime-setting repo wired for this purpose (the declared mode then always
  reads as standalone, which is the truth for those apps anyway). `env` is a **function**, not a
  captured value, because two of `DeploymentEnv`'s fields are only knowable per request: the
  cache/lock ping callbacks have to run against the LIVE store, and an app whose Settings editor
  just changed `cache.provider` must not keep reporting the value it booted with.
- `Preflight(w, r)` — `GET`. Calls `h.env()` (or an empty `DeploymentEnv{}` when `env` is nil) and
  `sharedservices.Preflight(ctx, env, h.state(r))`. Readable by ANY signed-in user, deliberately:
  it exposes provider names, a connection count, and the at-rest key's non-secret FINGERPRINT —
  never the key itself — and the operator who most needs this page is often looking before they
  have been granted anything else.
- `Mode(w, r)` — `GET`. Returns `h.state(r)` — the declared mode, echoed for the SPA in one round
  trip alongside the checklist.
- `SetMode(w, r)` — `POST`, a write, so callers register it behind whatever admin gate the app
  uses (the RBAC matrix, same as every other configuration change). Two refusals before the write
  is attempted:
  - `h.env().Appliance` — refused outright (`ErrBadRequest`, the appliance reason as the message).
    A stored `"clustered"` on an NVR would be a claim the product cannot honour, and the next reader
    could not tell it from a supported one.
  - `h.mode == nil` — refused with "deployment mode is not configurable on this app" (belt-and-braces
    alongside the appliance check, for a caller that somehow reaches this handler with no mode
    service wired at all).
  - Otherwise decodes `{mode, acknowledged}` from the body and delegates to `mode.Set`; the only
    error the service raises for a bad value (an unrecognised mode string) is reported as the
    caller's fault (`ErrBadRequest`), not the server's.
- `state(r)` — reads the declared mode via `h.mode.Get`, falling back to
  `DeploymentState{Mode: ModeStandalone}` on a `nil` mode service OR any read error. Deliberately
  not surfaced as an error to the caller: standalone is both the safe default and the truth for
  every install that never answered, so a transient read failure must not stop the checklist from
  rendering at all.

## Notes

- No secret ever crosses this surface. The at-rest row is a one-way HKDF fingerprint
  (`infra/atrest/cipher.go.md`), and the JWT-secret row reports provenance
  (`configured`/`generated`/`unset`) rather than any value or fingerprint of the secret itself —
  see `domain/shared/services/deployment.go.md`'s `checkJwtSecret` for why that one specifically
  carries no fingerprint.
- Per-app registration lives in `apps/<app>/apis/deployment.go` — Tier A apps
  (`myseliasan`, `myidsan`) mount all three routes explicitly behind `auth.Middleware`
  (+`AccessSessionMidware` for the write). Tier B apps (`mymatasan`, `myiotsan`, `mypintusan`)
  register only `GET /deployment/preflight`, with no explicit auth middleware of its own in
  `deployment.go` — but each is registered onto that app's already-authenticated `protected`
  subrouter (`NewLocalBasicAuth` + `NewRequireRolePermission`, wired once for the whole subrouter
  in `wire_routes.go`/`app.go`), so the route is reached only by a signed-in user regardless. There
  is deliberately no `POST /deployment/mode` on any Tier B app — the mode is not a choice there,
  and an endpoint that always refuses is a worse answer than one that was never offered. See each
  app's own `apis/deployment.go.md`.
