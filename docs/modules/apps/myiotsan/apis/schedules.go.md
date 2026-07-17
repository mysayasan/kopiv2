# Module: apps/myiotsan/apis/schedules.go

## Purpose

Registers schedules (time and sunrise/sunset triggers) under `/api/schedules`, plus the site
location (`GET`/`PUT /api/settings/location`) a sun trigger needs to compute its fire time. Thin
HTTP layer over `services.ScheduleService` (`services/schedules.go.md`); no gate lives here.

## Responsibilities

- `NewSchedulesApi(router, schedules)` mounts, all under `/schedules`:
  - `GET/POST /schedules`, `PUT/DELETE /schedules/{id}` — CRUD over
    `services.SaveScheduleRequest`. (There is no `GET /schedules/{id}` — `List` is the only read,
    matching how the frontend consumes it.)
  - `POST /schedules/{id}/run` — test-fires a schedule immediately via
    `services.ScheduleService.RunNow`, ignoring its trigger; still goes through the gated
    actuation path, so a test cannot do anything the schedule itself could not.
- Also mounted directly on `router` (not the `/schedules` subrouter), since it is not
  schedule-scoped: `GET`/`PUT /settings/location` — the site latitude/longitude.

## RBAC — reading vs authoring/running/location

Reading a schedule is granted to viewer/operator (`services.Policy()`). **Authoring, test-firing,
and setting the site location are all admin-only**: a schedule commands real devices, and its
location is the input that decides WHEN — both gated the same as issuing a command directly
(`services/rbac.go.md`).

## Notes

- Thin layer over `services.ScheduleService`; validation (`validate` — name required, a valid
  clock time or a set location for a sun trigger, a resolvable target) lives in the service, not
  here.
- Shares `readID`/`decode`/`actorId`/`actorName` helpers with the rest of the `apis` package
  (`apis/devices.go.md`). `POST /schedules/{id}/run` passes both through to `RunNow` so a
  tunnel-triggered test-fire is attributed by name, not "System".
