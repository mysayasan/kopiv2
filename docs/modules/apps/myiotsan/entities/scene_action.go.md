# Module: apps/myiotsan/entities/scene_action.go

## Purpose

One step of a `Scene` (`scene.go.md`): tell one device to run one of its profile's declared
commands with one value. `Ordinal` orders the steps; they run in that order, low to high.

## Fields

- `SceneId` (indexed `scene`) — the parent scene.
- `Ordinal` — the 0-based step order within the scene.
- `DeviceId`/`CommandName` — name the device and one of its declared commands. These are resolved
  at **run time**, not stored-resolved: a command whose bounds an admin later tightens is
  re-checked (`services.validateValue`) on every run, not frozen at authoring time — a scene
  cannot become a bypass of a bound that was tightened after it was authored.
- `Value` — the same single scalar a `DeviceCommand` carries (`entities/device_command.go.md`):
  a `dimmer` action holds a percentage, a `color` action holds the packed `0xRRGGBB` integer — so
  a scene inherits every command kind (`entities/profile_command.go.md`) for free and needs no
  per-kind machinery of its own.
- Audit fields: `CreatedBy`/`CreatedAt`/`UpdatedBy`/`UpdatedAt`.

## Notes

- Bootstrap creates this table from the registered entity (`app/app.go`'s `Entities()`).
- `services.SceneService.replaceActions` deletes then re-inserts a scene's whole action set on
  every save — same delete-then-insert-on-a-possibly-empty-table pattern
  `ProfileService.replaceCommands` uses (`services/profile.go.md`).
- `services.SceneService.runActions` issues one `commandService.Issue` per row, in `Ordinal`
  order, and never stops early on a refusal — see `services/scenes.go.md`.
