# Module: apps/myiotsan/apis/discovery.go

## Purpose

Registers onboarding under `/api/discovery`: the enrollment window, the candidates it collects,
and adoption/rejection. See `services/enrollment.go.md` for the design this is a thin HTTP layer
over.

Every route here is **ADMIN-ONLY** (`services/rbac.go.md`) — opening the window is the one act
in the whole app that lets an unknown thing talk to the broker at all, and an operator does not
get to do it.

## Responsibilities

- `NewDiscoveryApi(router, enroll, devices)` mounts:
  - `GET /discovery/window` — current `services.WindowStatus` (never carries the key).
  - `POST /discovery/window` — opens a window. Body is `{"minutes": N}`; the key is in this
    response and this response only, never readable back afterwards.
  - `DELETE /discovery/window` — closes the window immediately.
  - `GET /discovery/candidates` — the discovered-device list, chattiest first.
  - `POST /discovery/candidates/{id}/adopt` — body is `services.AdoptRequest`
    (`profileId`/`name`/`tag`/`location`); returns the new device's provisioned record,
    including its real, generated broker credential shown exactly once.
  - `DELETE /discovery/candidates/{id}` — discards a candidate.

## Notes

- Thin layer over `services.Enrollment`/`services.DeviceService`; no business logic lives here.
- Shares `readID`/`decode`/`actorId` helpers with the rest of the `apis` package
  (`apis/devices.go.md`).
