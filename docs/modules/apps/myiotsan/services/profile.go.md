# Module: apps/myiotsan/services/profile.go

## Purpose

Owns device types and the datapoints they report — the abstraction mymatasan does not have, and
the difference between a product and a demo: without it, onboarding a hundred identical door
sensors means configuring a hundred devices by hand; with it, the hundredth device is a name
and a profile.

## Key Type: ProfileService

```go
func NewProfileService(db dbsql.IDbCrud) *ProfileService
```

- `List`, `Detail(id) (*ProfileDetail, error)` (profile + its keys + its declared commands),
  `KeysFor(profileId)` — the latter is on the ingest path (every payload needs its bindings), so
  it is cached by `services.Ingest` rather than read per message.
- `Create(ctx, SaveProfileRequest, actor)` / `Update(...)` — a profile, its keys, AND its
  commands are saved in one call, and each is **replaced wholesale**, not diffed: a profile is a
  small declarative document, and an edit that half-applies is worse than one that replaces
  (`replaceKeys`/`replaceCommands` both delete then re-insert).
- `Delete(ctx, id)` — refuses with `ErrProfileBuiltin` if the profile is shipped; builtins can be
  used and copied but not removed.
- `EnsureBuiltins(ctx)` — seeds the shipped catalog (`profile_catalog.go.md`) on every boot,
  including each builtin's declared commands. Existing profiles are left ALONE (matched by
  `Slug`) — a site that has tuned a builtin's deadbands must not have that overwritten on the
  next boot, the same rule the RBAC seeder follows.

## Key Types: SaveProfileRequest / SaveProfileCommand / SaveTelemetryKey / ProfileDetail

Request/response DTOs for the profile CRUD API (`apis/profiles.go`). **(P5)**
`SaveProfileRequest` carries `Transport`/`ModbusMode`/`ModbusBase`/`PollSeconds` (see
`entities.DeviceProfile`) and `SaveTelemetryKey` carries `Register`/`RegKind`/`ScaleFactor`/
`WordSwap` (see `entities.TelemetryKey`) — both plumbed through `Create`/`Update`/`replaceKeys`/
`EnsureBuiltins` alongside the pre-existing MQTT-only fields, so a Modbus profile is saved,
edited and seeded through the identical path an MQTT one always has been. `SaveProfileCommand` (P4)
declares one command: `Name`/`Label`/`Kind` (`"switch"`/`"setpoint"`, and, since the
home-automation kinds, `"dimmer"`/`"position"`/`"cct"`/`"mode"`/`"color"` — see
`entities/profile_command.go.md`), `TopicTemplate`/`PayloadTemplate`, `Min`/`Max` (the safety
bounds, enforced server-side when a command is actually issued — see `services/commands.go.md`),
`Options` (a `"mode"` command's allowed `{value,label}` list, JSON, empty for every other kind),
and `ConfirmKey` (the telemetry key the device reports the resulting state back on; without it a
command can only ever be "sent", never "confirmed"). `ProfileDetail.Commands`
(`[]*entities.ProfileCommand`) rides alongside `Keys` in every profile detail response.

## Key Function: replaceCommands

```go
func (s *ProfileService) replaceCommands(ctx context.Context, profileId int64, cmds []SaveProfileCommand, actor int64) error
```

Deletes then re-inserts a profile's whole `ProfileCommand` set, same delete-then-insert-on-a-
possibly-empty-table pattern as `replaceKeys` (see the `isNoResultErr` note below — the same
"total affected: 0" trap applies here on a fresh table). Called from `Create`, `Update`, and
`EnsureBuiltins`.

## Notes

- `replaceKeys`'s delete-then-insert against a **fresh, empty** table is exactly what
  `EnsureBuiltins` does on first boot — this was the trigger for the "total affected: 0"
  panic fixed by `isNoResultErr` (see `device.go.md`).
- An edited profile must invalidate `services.Ingest`'s cached bindings for it
  (`apis.profilesApi.update`/`remove` call `ingest.InvalidateProfile(id)`) so a changed deadband
  takes effect on the next message, not the next restart.
