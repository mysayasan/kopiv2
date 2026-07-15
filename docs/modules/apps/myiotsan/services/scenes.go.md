# Module: apps/myiotsan/services/scenes.go

## Purpose

Owns scenes — named, ordered groups of device commands — and running them. Running a scene is
**not** a new way to command a device: it is a loop that calls `CommandService.Issue` once per
action, so every gate, every audit row and every twin update happens exactly as if a person had
issued each command by hand. The service re-implements no gate; it only sequences. See
`docs/MYIOTSAN_PLAN.md` §8h.

## Key Type: commandIssuer

```go
type commandIssuer interface {
    Issue(ctx context.Context, deviceId int64, req IssueRequest, actor int64, actorName string) (*entities.DeviceCommand, error)
}
```

The one thing a scene needs from the actuation layer: the single gated entry point that issues a
command. Depending on the interface rather than `*CommandService` keeps `Run` unit-testable
without a broker or a database — `*CommandService` satisfies it in production
(`services/commands.go.md`), a `fakeIssuer` in `scenes_test.go`.

## Key Type: SceneService

```go
func NewSceneService(db dbsql.IDbCrud, commands *CommandService, logf func(string, ...any)) *SceneService

func (s *SceneService) List(ctx) ([]*entities.Scene, error)
func (s *SceneService) Detail(ctx, id) (*SceneDetail, error)
func (s *SceneService) Create(ctx, SaveSceneRequest, actor) (*SceneDetail, error)
func (s *SceneService) Update(ctx, id, SaveSceneRequest, actor) (*SceneDetail, error)
func (s *SceneService) Delete(ctx, id) error
func (s *SceneService) Run(ctx, sceneId, actor, actorName) (*SceneRunResult, error)
```

- `SaveSceneRequest`/`SaveSceneAction` — CRUD DTOs for `apis/scenes.go`. `Create`/`Update` save a
  scene and its whole action set in one call, and the actions are **replaced wholesale**
  (`replaceActions`: delete then re-insert), not diffed — same "a small declarative document, an
  edit that half-applies is worse than one that replaces" rule `ProfileService` follows for a
  profile's keys/commands (`profile.go.md`).
- `Run` refuses a disabled scene (`ErrScene not found`-style plain errors, not disabled-and-silent)
  before running anything, then delegates the fan-out to `runActions`.

## Key Function: runActions / SceneRunResult

```go
func (s *SceneService) runActions(ctx, sceneId, actions []*entities.SceneAction, actor, actorName) *SceneRunResult
```

Issues each action in **order** and collects its outcome. **Never stops early and never rolls
back**: a physical action cannot be undone, so the honest thing is to run every action and report
each result. `SceneRunResult.Results` (`[]ActionResult`) carries each action's `Status`/`Error`
verbatim from the `DeviceCommand` `Issue` returned — a refused action ("actuation is not enabled
for this device") reads in a scene run exactly as it would from a manual command. Extracted from
`Run` so this fan-out + partial-failure behaviour is unit-testable without a database
(`scenes_test.go.md`).

**A hazard worth reading `Issue`'s doc for**: the 2-second per-device rate limit
(`services/commands.go.md`) applies across a scene's actions too. Two actions in the same scene
targeting the SAME device inside two seconds will see the second one refused — surfaced in the
report as a normal failed `ActionResult`, not hidden. Verified live: a scene run's per-action
report included exactly this rate-limit refusal.

## Notes

- Wired in `app.go`'s `RegisterAppRoutes`, constructed right after actuation
  (`services.NewSceneService(deps.Db, commandService, logf)`), before `ScheduleService`
  (`services/schedules.go.md`), which depends on it to run a scene-targeted schedule. See
  `app/app.go.md`.
- Exposed over HTTP by `apis/scenes.go.md`. RBAC: readable by viewer/operator, running one is
  admin-only (`services/rbac.go.md`) — running a scene commands real devices, so it inherits the
  same line a single command draws.
- `services.ScheduleService.fireAs` calls `Run` for a scene-targeted schedule, passing through the
  synthetic `"schedule:<name>"` actor (`services/schedules.go.md`).
