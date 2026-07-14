# Module: apps/myiotsan/apis/commands.go

## Purpose

Registers actuation under `/api/devices/{id}/commands` and `/twin`. Thin HTTP layer over
`services.CommandService` (`services/commands.go.md`) — every safety gate lives there, not here.

## Responsibilities

- `NewCommandsApi(router, commands, devices)` mounts, all under `/devices/{id}`:
  - `GET /commands` — what this device can be told to do (exactly its profile's declared
    commands), plus `actuationEnabled` riding along so the UI can explain WHY a device with
    declared commands still cannot be commanded, rather than just greying a button out with no
    reason.
  - `POST /commands` — issue one (`services.IssueRequest`: `name`/`value`). A refusal is still
    returned with a `400` **and the reason verbatim** (e.g. "outside the safe range 5..30") —
    the recorded `DeviceCommand` carries the same audit row either way. The success response is
    the command in `"sent"` status — **not** "done"; the caller must not render it as confirmed.
  - `GET /commands/history` — the audit trail, paginated, newest first.
  - `GET /twin` — desired vs reported state for the device; the gap between them is the only
    honest answer to "is the door locked?".

## RBAC — why history and twin are not admin-only

Every **write** route here (`POST /commands`) is admin-only (`services/rbac.go.md`, unchanged
since P0). `GET /commands/history` and `GET /twin` are deliberately **not**: seeing what was done
to a device, and whether the door is actually locked, is not the same power as doing it. An audit
trail visible only to the people who could have written to it is not an audit trail. This is
expressible because `services.Policy()`'s matrix is most-specific-path-wins, so
`/api/devices/*/commands/history` and `/api/devices/*/twin` can grant `read` to viewer/operator
while the shorter `/api/devices/*/commands` prefix stays admin-only.

## Notes

- Thin layer over `services.CommandService`/`services.DeviceService`; no gates live here.
- Shares `readID`/`decode`/`readPaging`/`actorId`/`actorName` helpers with the rest of the `apis`
  package (`apis/devices.go.md`). `POST /commands` passes both `actorId(r)` and `actorName(r)`
  through to `Issue` so a command issued over the fleet tunnel is attributed to the control-plane
  operator by name, not recorded as "System".
