# Module: apps/mymatasan/apis/deployment.go

## Purpose

`NewDeploymentApi` — mymatasan's registration of the shared deployment-mode surface
(`domain/shared/apis/deployment.go.md`), fixed to a read-only appliance answer.

## Responsibilities

- `NewDeploymentApi(router)` mounts only `GET /deployment/preflight`, built from a static
  `sharedservices.DeploymentEnv{Appliance: true, ApplianceReason: sharedservices.ApplianceLocalMedia}`
  — no `mode` service (`nil`) and no `POST /deployment/mode` route.
- The answer is fixed: mymatasan owns its camera capture pipelines, writes recordings to local
  disk, and pins detection to this host's GPU. A second instance would not share that work — it
  would open its own streams against the same cameras and write a second, divergent set of
  recordings. Availability for an NVR is a redundancy question (a second recorder, RAID, a spare
  host) rather than a load-balancer one, which is what the UI says instead. There is deliberately
  no `POST` route — see mypintusan's equivalent.
- Registered on the already-authenticated `protected` subrouter (`apps/mymatasan/app/wire_routes.go.md`,
  right after `apis.NewSetupApi`) — the handler itself carries no explicit auth middleware because
  the subrouter's already covers it.

## Notes

- See `domain/shared/services/deployment.go.md`'s tiering for why `mymatasan` is Tier B alongside
  `myiotsan`/`mypintusan`, and `mypintusan/apis/deployment.go.md`'s / `myiotsan/apis/deployment.go.md`'s
  equivalents for the identical pattern applied to a different single-host constraint (a serial bus
  rather than local media/GPU).
