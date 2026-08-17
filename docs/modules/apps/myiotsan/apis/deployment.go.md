# Module: apps/myiotsan/apis/deployment.go

## Purpose

`NewDeploymentApi` — myiotsan's registration of the shared deployment-mode surface
(`domain/shared/apis/deployment.go.md`), fixed to a read-only appliance answer.

## Responsibilities

- `NewDeploymentApi(router)` mounts only `GET /deployment/preflight`, built from a static
  `sharedservices.DeploymentEnv{Appliance: true, ApplianceReason: sharedservices.ApplianceSerialBus}`
  — no `mode` service (`nil`) and no `POST /deployment/mode` route.
- The answer is fixed: myiotsan's Modbus RTU pollers open serial ports (`COM3`, `/dev/ttyUSB0`)
  that exactly one process on one host can hold, and the Modbus TCP pollers keep long-lived device
  sessions a second instance would contend for rather than share.
- Registered on the already-authenticated `protected` subrouter (`apps/myiotsan/app/app.go.md`,
  step 8), right after `apis.NewSetupApi` — the handler itself carries no explicit auth middleware
  because the subrouter's already covers it.

## Notes

- See `domain/shared/services/deployment.go.md`'s tiering for why `myiotsan` is Tier B alongside
  `mymatasan`/`mypintusan`, and `mypintusan/apis/deployment.go.md`'s equivalent for the identical
  pattern applied to a different serial bus (OSDP rather than Modbus RTU).
