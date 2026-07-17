# Module: apps/myiotsan/apis/discovery.go

## Purpose

Registers onboarding under `/api/discovery`: the enrollment window, an active network scan, the
candidates both collect, and adoption/rejection. See `services/enrollment.go.md` for the
announce-path design and `services/scanner.go.md` for the active-scan path this is a thin HTTP
layer over.

Every route here is **ADMIN-ONLY** (`services/rbac.go.md`) — opening the window (or running a
scan) is the one act in the whole app that lets an unknown thing onto the candidate list at all,
and an operator does not get to do it.

## Responsibilities

- `NewDiscoveryApi(router, enroll, devices, scan)` mounts:
  - `GET /discovery/window` — current `services.WindowStatus` (never carries the key).
  - `POST /discovery/window` — opens a window. Body is `{"minutes": N}`; the key is in this
    response and this response only, never readable back afterwards.
  - `DELETE /discovery/window` — closes the window immediately.
  - `POST /discovery/scan` — runs an active network scan. Body is `services.ScanRequest`
    (`types`/`cidr`/`modbusPort`/`modbusTransport`/`units`); returns `services.ScanResult`
    (`found`/`byType`) — the actual candidates land in `GET /discovery/candidates`, same as an
    announced device. The counterpart to the enrollment window: its results feed the SAME
    candidate list.
  - `GET /discovery/candidates` — the discovered-device list, chattiest first — now a mix of
    MQTT-announced and scan-found candidates, distinguished by `Source`.
  - `POST /discovery/candidates/{id}/adopt` — body is `services.AdoptRequest`
    (`profileId`/`name`/`tag`/`location`); returns the new device's provisioned record,
    including its real, generated broker credential shown exactly once. For a Modbus-scan
    candidate, the created device also inherits its endpoint/unit/transport (see
    `services/enrollment.go.md`'s `Adopt`).
  - `DELETE /discovery/candidates/{id}` — discards a candidate.

## Notes

- Thin layer over `services.Enrollment`/`services.DeviceService`/`services.ScanService`; no
  business logic lives here — `runScan` just decodes the body, calls `scan.Scan`, and returns the
  summary or a `400` with the service's error text (e.g. "a Modbus scan needs a network range").
- Shares `readID`/`decode`/`actorId` helpers with the rest of the `apis` package
  (`apis/devices.go.md`).
