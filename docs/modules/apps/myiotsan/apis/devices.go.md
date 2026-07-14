# Module: apps/myiotsan/apis/devices.go

## Purpose

Registers the device inventory and its telemetry under `/api/devices`.

## Responsibilities

- `NewDevicesApi(router, devices, telemetry, profiles, ingest)` mounts:
  - `GET/POST /devices` — list / create.
  - `GET /devices/stats` — registered **before** `/devices/{id}` so the literal path wins over
    the id pattern. Exposes `services.IngestStats` (received/decoded/stored/suppressed/written/
    dropped/queued/series) — the ratio of suppressed to stored IS the storage design working or
    not, and `dropped > 0` means the disk cannot keep up; both are things an operator has to be
    able to see rather than infer.
  - `GET/PUT/DELETE /devices/{id}` — read / edit / remove.
  - `POST /devices/{id}/password` — rotate the device's broker credential (admin-only per
    `services/rbac.go`).
  - `GET /devices/{id}/readings` — a time series for one `key` (query param), with `from`/`to`
    unix-second bounds; what the device chart reads. Operator-and-up per RBAC.
  - `GET /devices/{id}/latest` — current value of every key; what the device page header shows.
- `create` returns the generated broker password in the response body **and nowhere else,
  ever**.
- Shared helpers used across this package: `readPaging` (limit/offset query parsing),
  `readID` (mux var → validated int64), `decode` (1MB-capped, `DisallowUnknownFields` JSON
  body decode), `actorId` (resolves the signed-in local user for created_by/updated_by).

## Notes

- Thin HTTP layer over `services.DeviceService`/`TelemetryService`/`ProfileService`/`Ingest`;
  no business logic lives here.
