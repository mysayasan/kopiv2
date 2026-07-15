# Module: apps/myiotsan/apis/scenes.go

## Purpose

Registers scenes — named groups of device commands — and running them, under `/api/scenes`. Thin
HTTP layer over `services.SceneService` (`services/scenes.go.md`); no gate lives here.

## Responsibilities

- `NewScenesApi(router, scenes)` mounts, all under `/scenes`:
  - `GET/POST /scenes`, `GET/PUT/DELETE /scenes/{id}` — CRUD over `services.SaveSceneRequest`.
  - `POST /scenes/{id}/run` — fans the scene out through `services.SceneService.Run`, returning
    a per-action `SceneRunResult`. A partial failure is a `200` with some actions failed, **not**
    an error response — the operator needs to see WHICH action acted and which did not, not just
    that "something failed".

## RBAC — reading vs running

Reading and authoring a scene are ordinary CRUD, and (per `services.Policy()`) reading is granted
to viewer/operator. **Running one is admin-only**, for the identical reason a single command is:
running a scene commands real devices through `CommandService.Issue`, so `/api/scenes/*/run` is
denied to everyone below admin in the matrix (`services/rbac.go.md`).

## Notes

- Thin layer over `services.SceneService`; no validation or gate logic lives here.
- Shares `readID`/`decode`/`actorId`/`actorName` helpers with the rest of the `apis` package
  (`apis/devices.go.md`). `POST /scenes/{id}/run` passes both through to `Run` so a run triggered
  over the fleet tunnel is attributed to the control-plane operator by name in every resulting
  `DeviceCommand` row, not recorded as "System".
