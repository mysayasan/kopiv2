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
  - `GET/PUT/DELETE /devices/{id}` — read / edit / remove. `remove` also calls
    `ingest.ForgetDevice(id)` (`services/ingest.go.md`) so the deadband gate's baseline for this
    device does not outlive it.
  - `POST /devices/{id}/password` — rotate the device's broker credential (admin-only per
    `services/rbac.go`).
  - `GET /devices/{id}/readings` — a time series for one `key` (query param), with `from`/`to`
    unix-second bounds, capped at `seriesMaxPoints` (2000); what the device chart reads.
    Operator-and-up per RBAC. Answers `{items, span, truncated}` — `span` is `"raw"`/`"1m"`/
    `"1h"` (the resolution the points actually carry) and `truncated` is true only when the
    window held more raw points than the cap and no rollup covered it yet. See
    `services/telemetry.go.md`'s `SeriesPage`.
  - `GET /devices/{id}/latest` — current value of every key; what the device page header shows.
    Resolves the calling device's profile and passes its declared key list into
    `TelemetryService.Latest` — a device with no profile (or an unresolvable one) passes no keys
    and `Latest` falls back to its tail scan.
- `create` returns the generated broker password in the response body **and nowhere else,
  ever**.
- Shared helpers used across this package: `readPaging` (limit/offset query parsing),
  `readID` (mux var → validated int64), `decode` (1MB-capped, `DisallowUnknownFields` JSON
  body decode), `actorId` (resolves the signed-in local user for created_by/updated_by),
  `actorName` (the same caller, by name — not redundant with `actorId`: a caller can arrive over
  the fleet tunnel from a control-plane operator with no local account, so `actorId` is `0` but
  the tunnel's synthetic principal still carries `cp:<who>`; used by `commands.Issue` and
  `rules.AckAlert` so the audit trail names a person instead of "System").

## Notes

- Thin HTTP layer over `services.DeviceService`/`TelemetryService`/`ProfileService`/`Ingest`;
  no business logic lives here.
