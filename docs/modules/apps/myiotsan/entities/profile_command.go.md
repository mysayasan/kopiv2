# Module: apps/myiotsan/entities/profile_command.go

## Purpose

Declares something a device TYPE can be told to do, and the bounds within which it may be told
to do it. A device can only be commanded in ways its own profile declares — there is no generic
"publish this arbitrary payload to that topic" endpoint anywhere in the app, and that is
deliberate: an escape hatch like that would turn the appliance into a remote shell for the
building's electrics, and every safety property below would be a suggestion rather than a rule.
See `services/commands.go.md` and `docs/MYIOTSAN_PLAN.md` §3.4.

## Fields

- `ProfileId` (indexed `profile`) — the device type this command belongs to.
- `Name` — the command's identifier (`"output"`, `"setpoint"`); `Label` — display text; `Kind` —
  `"switch"` (0/1 only) or `"setpoint"` (a number bounded by `Min..Max`).
- `TopicTemplate` — where the command is published, `{deviceKey}` substituted.
- `PayloadTemplate` — the message body, `{value}` substituted (JSON in practice, e.g.
  `{"method":"Switch.Set","params":{"id":0,"on":{value}}}`). An empty template sends the bare
  value — for a device whose command topic IS the instruction.
- `Min`/`Max` — bound a setpoint, **enforced server-side** (`services.validateValue`), not merely
  rendered as input attributes in a form. "The frontend validates it" is not a safety property; a
  thermostat that accepts 200°C because a UI slider was bypassed is a fire. A setpoint declaring
  `Min == 0 && Max == 0` (i.e. no range at all) refuses **every** value — an unbounded setpoint on
  a physical device is an omission, not permission, and the safe reading of an omission is no.
- `ConfirmKey` — the telemetry key the device reports the resulting state back on, and what
  actually confirms a command. Without it, "sent" is the best that can ever be said, and "sent"
  is not "it happened". A relay command whose device reports `output` back is confirmed only
  when a reading on `output` matches the value that was asked for.
- Audit fields: `CreatedBy`/`CreatedAt`/`UpdatedBy`/`UpdatedAt`.

## Notes

- `services.ProfileService.replaceCommands` rewrites a profile's whole command set on save
  (delete-then-insert, same pattern as `replaceKeys` in `profile.go.md`) — a profile is a small
  declarative document, and an edit that half-applies is worse than one that replaces.
- Most of the shipped catalog (`profile_catalog.go.md`) declares no commands at all, and that is
  the correct default: a sensor that cannot be commanded cannot be commanded wrongly. `smart-relay`
  is currently the one profile that declares any.
- Bootstrap creates this table from the registered entity (`app/app.go`'s `Entities()`).
