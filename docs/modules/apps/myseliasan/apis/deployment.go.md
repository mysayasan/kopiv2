# Module: apps/myseliasan/apis/deployment.go

## Purpose

`NewDeploymentApi` — myseliasan's registration of the shared deployment-mode/readiness surface
(`domain/shared/apis/deployment.go.md`). myseliasan is the other of the two apps in the suite
genuinely able to run behind a load balancer (stateless over its database — see
`domain/shared/services/deployment.go.md`'s tiering), though see `app/app.go.md`'s "Two known
gaps" for what a declared-clustered install still gets wrong (heartbeat/liveness, cross-instance
fleet rules).

## Responsibilities

- `NewDeploymentApi(router, auth, access, mode, env)` mounts under `/deployment`, identically to
  `myidsan`'s: `GET /deployment/preflight` and `GET /deployment/mode` behind `auth.Middleware`
  only; `POST /deployment/mode` behind `auth.Middleware` + `access.Middleware` (the RBAC matrix,
  here `controlSession`) on its own subrouter.
- Wired in `app/app.go.md` alongside the other Tier-A-specific bits: the `DeploymentEnv` closure
  is rebuilt per request (not captured) so the Settings editor changing `cache.provider` is
  reflected without a restart, and its `ExtraChecks` field is set to `llmSidecarDeploymentCheck(deps)`
  — myseliasan's one app-specific preflight row, warning that `agent.llm.mode == "sidecar"` costs
  each instance its own copy of the model in memory and its own separate download, rather than
  every instance sharing one external inference endpoint. It is a WARNING, not a BLOCKER: the
  deployment still works, it is just wasteful, and an operator who genuinely wants a model per
  instance is entitled to that.

## Notes

- See `apps/myseliasan/app/app.go.md` for the full `DeploymentEnv` closure (`DbEngine`,
  `CacheProvider`, `LockProvider`, `JwtSecret`/`JwtSecretGenerated`, `MaxOpenConns`,
  `AtRestEnabled`/`AtRestFingerprint` — here the fleet CA key/PSK's fingerprint —
  `CachePing`/`LockPing`, `ExtraChecks`).
- `mode` is a real `sharedservices.NewDeploymentModeService(runtimeSettingRepo)`.
- Consumed by the setup wizard's new Deployment step (position 1, before sign-in and node
  adoption) and by the Settings panel — both render the shared `frontend/shared/src/Deployment.js`
  component. See `apps/myseliasan/README.md`.
