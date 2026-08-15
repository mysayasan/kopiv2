# Module: apps/myidsan/apis/deployment.go

## Purpose

`NewDeploymentApi` — myidsan's registration of the shared deployment-mode/readiness surface
(`domain/shared/apis/deployment.go.md`). Matters more here than anywhere else in the suite:
myidsan is the identity provider, so when it is down every other app's sign-in is down with it.
It is also one of the two apps in the suite genuinely able to run behind a load balancer (stateless
over its database — see `domain/shared/services/deployment.go.md`'s tiering).

## Responsibilities

- `NewDeploymentApi(router, auth, access, mode, env)` mounts under `/deployment`:
  - `GET /deployment/preflight` — behind `auth.Middleware` only. Readable by any signed-in user;
    carries no secret (the at-rest row is a one-way fingerprint, never the key).
  - `GET /deployment/mode` — behind `auth.Middleware` only.
  - `POST /deployment/mode` — behind `auth.Middleware` + `access.Middleware` (the RBAC matrix), on
    a separate `/deployment/mode` subrouter so the write gate does not also cover the two reads.
- Wired in `app/app.go.md`, right after `secretKeyStore` is resolved — the checklist's
  `atrestKey` row reports `secretKeyStore.Fingerprint()`, so the deployment API is registered only
  once the cipher exists to fingerprint.

## Notes

- See `apps/myidsan/app/app.go.md` for the `DeploymentEnv` closure this app builds per request
  (`DbEngine`, `CacheProvider`, `LockProvider`, `JwtSecret`/`JwtSecretGenerated`, `MaxOpenConns`,
  `AtRestEnabled`/`AtRestFingerprint`, `CachePing`/`LockPing` via `sharedservices.PingFunc`).
- `mode` is a real `sharedservices.NewDeploymentModeService(runtimeSettingRepo)` — myidsan is one
  of the two apps that can genuinely declare `clustered`.
