# Module: apps/myiotsan/entities/scene.go

## Purpose

A named, ordered set of device commands run together — "movie night", "all off", "goodnight".
Running a scene is convenience, not a new authority: it fans its actions out through the exact
same actuation chokepoint a single manual command takes (`services.CommandService.Issue`), so
every gate (actuation-enabled by device, admin-only to run, profile-declared bounds, rate limit,
audit, twin confirmation, never auto-retried) applies per action. See `services/scenes.go.md` and
`docs/MYIOTSAN_PLAN.md` §8h.

## Fields

- `Name` — required. `Description` — free text.
- `Enabled` — a soft off switch; a disabled scene is kept and editable but refuses to run
  (`services.SceneService.Run`) and is not offered to a schedule as a target.
- Audit fields: `CreatedBy`/`CreatedAt`/`UpdatedBy`/`UpdatedAt`.

## Notes

- A scene's steps live in a separate table, `SceneAction` (`scene_action.go.md`), one row per
  step, `Ordinal`-ordered — the same "one parent row, N ordered/replaced child rows" shape
  `DeviceProfile`/`ProfileCommand` already use.
- `services.SceneService.replaceActions` rewrites a scene's whole action set on save
  (delete-then-insert) — a scene is a small declarative document, and an edit that half-applies is
  worse than one that replaces.
- Bootstrap creates this table from the registered entity (`app/app.go`'s `Entities()`).
- Referenced by `Schedule.SceneId` (`schedule.go.md`) when a schedule's target is a scene rather
  than a single command.
