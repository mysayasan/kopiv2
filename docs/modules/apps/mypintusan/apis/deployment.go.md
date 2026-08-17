# Module: apps/mypintusan/apis/deployment.go

## Purpose

`NewDeploymentApi` — mypintusan's registration of the shared deployment-mode surface
(`domain/shared/apis/deployment.go.md`), fixed to a read-only appliance answer.

## Responsibilities

- `NewDeploymentApi(router)` mounts only `GET /deployment/preflight`, built from a static
  `sharedservices.DeploymentEnv{Appliance: true, ApplianceReason: sharedservices.ApplianceSerialBus}`
  — no `mode` service (`nil`) and no `POST /deployment/mode` route.
- The answer is fixed: mypintusan's `osdp.Bus` opens its serial port once and holds it for the
  bus's lifetime. A second instance would not share the doors, it would fail to open the port at
  all. There is deliberately no `POST` route — the mode is not a choice here, and an endpoint that
  always refuses is a worse answer than one that was never offered.
- Registered on the already-authenticated `protected` subrouter (`apps/mypintusan/app/app.go.md`,
  step 8), alongside `apis.NewSetupApi` — the handler itself carries no explicit auth middleware
  because the subrouter's already covers it.

## Notes

- See `domain/shared/services/deployment.go.md`'s tiering for why `mypintusan` is Tier B alongside
  `mymatasan`/`myiotsan`, and `myiotsan/apis/deployment.go.md`'s equivalent for the identical
  pattern applied to a different serial bus (Modbus RTU rather than OSDP).
