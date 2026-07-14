# Module: apps/myiotsan/entities/device_command.go

## Purpose

One attempt to make a device DO something — and the audit record of that attempt, not merely a
queue entry. A row here answers "who unlocked that door, when, and did it actually open?", which
is the question somebody will eventually ask. See `services/commands.go.md` for the gates that
produce this row and `docs/MYIOTSAN_PLAN.md` §3.4.

## Fields

- `DeviceId` (indexed `dev_time` with `RequestedAt`), `DeviceName` — denormalized so the audit
  trail survives the device being deleted; an audit log with dangling references is not evidence.
- `Name` — the command the profile declares (`"output"`, `"setpoint"`); `Value` — what was asked
  for.
- `Status` — the lifecycle, and the one field an operator actually reads:
  - `pending` — accepted, not yet published.
  - `sent` — published to the device. **Not** "done" — see `Error`/notes below.
  - `confirmed` — the device REPORTED BACK the state that was asked for. The only status that
    means the physical thing actually happened.
  - `failed` — refused (a gate rejected it), or never confirmed within the window.
  - A failed command is **never retried automatically**. Re-sending a relay write is a SECOND
    PHYSICAL ACTION: if the first one landed but its confirmation was lost, a retry fires the
    relay again — the door opens twice — and nothing at this layer can tell the two cases apart.
    A timeout ends the command and leaves the decision to a human; see
    `CommandService.SweepUnconfirmed`.
- `Error` — explains a refusal or an unconfirmed timeout in words an operator can act on (e.g.
  "outside the safe range 5..30", "the device never reported the new state — it may or may not
  have acted. Not retried automatically: re-sending could act twice.").
- `RequestedBy`/`RequestedAt` — the local user id and when; `SentAt`; `ConfirmedAt` (zero means
  never confirmed — an unconfirmed command must never be displayed as if it succeeded).
- `RequestedByName` — who `RequestedBy` WAS, by name, and not redundant with the id. A command can
  arrive over the **fleet tunnel** from an operator on the myseliasan control plane; that operator
  has no account on this node, so `RequestedBy` is `0`. The tunnel carries their name on the
  synthetic principal (`cp:<who>`), and `apis.actorName(r)` records it here — otherwise this
  node's own audit trail, the first place an investigator looks, would say a relay was switched by
  "System" when it was switched by a person. Verified live through the tunnel.

## Notes

- A row is written for **every** attempt, including refusals — "somebody tried to unlock the
  front door at 03:00 and was refused" is exactly what must not be thrown away just because it
  did not succeed.
- Bootstrap creates this table from the registered entity (`app/app.go`'s `Entities()`).
- `services.CommandService` (`services/commands.go.md`) owns the full lifecycle; `apis/commands.go.md`
  exposes it read-only (history) to viewer and operator, write-only (issuing) to admin.
