# Module: apps/myiotsan/services/scenes_test.go

## Purpose

Pins the pure, unit-testable half of `SceneService.Run` — the ordered fan-out and its
partial-failure behaviour — without a broker or a database.

## Responsibilities

- `fakeIssuer` — a `commandIssuer` (`scenes.go.md`) test double that records every call it
  receives (device id + request) and can be told to refuse commands for one specific device id,
  standing in for `*CommandService` so a scene run is testable in isolation.
- `TestScene_RunsEveryActionInOrder` — three actions across three devices are issued in `Ordinal`
  order, each producing a `"sent"` `ActionResult` that matches what was asked for.
- `TestScene_PartialFailureDoesNotAbortTheRest` — the middle action's device is made to refuse;
  all three actions are still attempted (a refusal does not stop the run), and the report shows
  the refused action as `"failed"` with a reason while the two around it still read `"sent"`.

## Notes

- Exercises `SceneService.runActions` directly against a bare `SceneService{commands: f, logf:
  ...}` — no database, no `NewSceneService`, no HTTP.
- The database-dependent half (CRUD, `replaceActions`, `Detail`) is exercised live instead — see
  `app/app.go.md`'s home-automation verification note.
