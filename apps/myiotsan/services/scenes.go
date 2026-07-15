package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// commandIssuer is the one thing a scene needs from the actuation layer: the single gated entry
// point that issues a command. Depending on the interface rather than *CommandService keeps Run
// unit-testable without a broker or a database — CommandService satisfies it in production.
type commandIssuer interface {
	Issue(ctx context.Context, deviceId int64, req IssueRequest, actor int64, actorName string) (*entities.DeviceCommand, error)
}

// SceneService owns scenes — named, ordered groups of device commands — and running them.
//
// Running a scene is NOT a new way to command a device: it is a loop that calls CommandService.Issue
// once per action, so every gate, every audit row and every twin update happens exactly as if a
// person had issued each command by hand. The service re-implements no gate; it only sequences.
type SceneService struct {
	scenes   dbsql.IGenericRepo[entities.Scene]
	actions  dbsql.IGenericRepo[entities.SceneAction]
	commands commandIssuer
	logf     func(format string, args ...any)
}

func NewSceneService(db dbsql.IDbCrud, commands *CommandService, logf func(string, ...any)) *SceneService {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &SceneService{
		scenes:   dbsql.NewGenericRepo[entities.Scene](db),
		actions:  dbsql.NewGenericRepo[entities.SceneAction](db),
		commands: commands,
		logf:     logf,
	}
}

// SceneDetail is a scene with its ordered actions.
type SceneDetail struct {
	Scene   *entities.Scene         `json:"scene"`
	Actions []*entities.SceneAction `json:"actions"`
}

// SaveSceneRequest creates or replaces a scene and its actions in one call — the same
// replace-children shape a profile uses.
type SaveSceneRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Enabled     bool              `json:"enabled"`
	Actions     []SaveSceneAction `json:"actions"`
}

type SaveSceneAction struct {
	DeviceId    int64   `json:"deviceId"`
	CommandName string  `json:"commandName"`
	Value       float64 `json:"value"`
}

func (s *SceneService) List(ctx context.Context) ([]*entities.Scene, error) {
	rows, _, err := s.scenes.Get(ctx, "", 500, 0, nil,
		[]sqldataenums.Sorter{{FieldName: "Name", Sort: sqldataenums.ASC}})
	if err != nil && isNoResultErr(err) {
		return nil, nil
	}
	return rows, err
}

func (s *SceneService) Detail(ctx context.Context, id int64) (*SceneDetail, error) {
	scene, err := s.scenes.GetById(ctx, "", uint64(id))
	if err != nil || scene == nil {
		return nil, fmt.Errorf("scene not found")
	}
	actions, err := s.actionsFor(ctx, id)
	if err != nil {
		return nil, err
	}
	return &SceneDetail{Scene: scene, Actions: actions}, nil
}

func (s *SceneService) actionsFor(ctx context.Context, sceneId int64) ([]*entities.SceneAction, error) {
	rows, _, err := s.actions.Get(ctx, "", 500, 0,
		[]sqldataenums.Filter{{FieldName: "SceneId", Compare: sqldataenums.Equal, Value: sceneId}},
		[]sqldataenums.Sorter{{FieldName: "Ordinal", Sort: sqldataenums.ASC}})
	if err != nil && isNoResultErr(err) {
		return nil, nil
	}
	return rows, err
}

func (s *SceneService) Create(ctx context.Context, req SaveSceneRequest, actor int64) (*SceneDetail, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("a scene needs a name")
	}
	now := time.Now().Unix()
	id, err := s.scenes.Create(ctx, "", entities.Scene{
		Name: name, Description: strings.TrimSpace(req.Description), Enabled: req.Enabled,
		CreatedBy: actor, CreatedAt: now, UpdatedBy: actor, UpdatedAt: now,
	})
	if err != nil {
		return nil, err
	}
	if err := s.replaceActions(ctx, int64(id), req.Actions, actor); err != nil {
		return nil, err
	}
	return s.Detail(ctx, int64(id))
}

func (s *SceneService) Update(ctx context.Context, id int64, req SaveSceneRequest, actor int64) (*SceneDetail, error) {
	existing, err := s.scenes.GetById(ctx, "", uint64(id))
	if err != nil || existing == nil {
		return nil, fmt.Errorf("scene not found")
	}
	existing.Name = strings.TrimSpace(req.Name)
	existing.Description = strings.TrimSpace(req.Description)
	existing.Enabled = req.Enabled
	existing.UpdatedBy = actor
	existing.UpdatedAt = time.Now().Unix()
	if existing.Name == "" {
		return nil, fmt.Errorf("a scene needs a name")
	}
	if _, err := s.scenes.UpdateById(ctx, "", *existing); err != nil {
		return nil, err
	}
	if err := s.replaceActions(ctx, id, req.Actions, actor); err != nil {
		return nil, err
	}
	return s.Detail(ctx, id)
}

func (s *SceneService) Delete(ctx context.Context, id int64) error {
	if _, err := s.actions.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "SceneId", Compare: sqldataenums.Equal, Value: id}}); err != nil && !isNoResultErr(err) {
		return err
	}
	_, err := s.scenes.DeleteById(ctx, "", uint64(id))
	return err
}

func (s *SceneService) replaceActions(ctx context.Context, sceneId int64, actions []SaveSceneAction, actor int64) error {
	if _, err := s.actions.Delete(ctx, "",
		[]sqldataenums.Filter{{FieldName: "SceneId", Compare: sqldataenums.Equal, Value: sceneId}}); err != nil && !isNoResultErr(err) {
		return err
	}
	now := time.Now().Unix()
	for i, a := range actions {
		name := strings.TrimSpace(a.CommandName)
		if a.DeviceId <= 0 || name == "" {
			continue
		}
		if _, err := s.actions.Create(ctx, "", entities.SceneAction{
			SceneId: sceneId, Ordinal: i, DeviceId: a.DeviceId, CommandName: name, Value: a.Value,
			CreatedBy: actor, CreatedAt: now, UpdatedBy: actor, UpdatedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

// ActionResult is the outcome of one action in a run. It carries the command's own status/error
// verbatim, so a refused action ("actuation is not enabled for this device") reads the same in a
// scene run as it would from a manual command.
type ActionResult struct {
	DeviceId    int64  `json:"deviceId"`
	CommandName string `json:"commandName"`
	Value       float64 `json:"value"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

// SceneRunResult is the per-action report of a run. Partial success is FIRST-CLASS: a scene never
// rolls back (a physical action cannot be undone) and never stops early on a refusal — it runs every
// action and reports each outcome, so an operator sees exactly what happened, not just that
// "something failed".
type SceneRunResult struct {
	SceneId int64          `json:"sceneId"`
	Results []ActionResult `json:"results"`
}

// Run executes a scene's actions in order, each through CommandService.Issue. actor/actorName are
// threaded so the audit trail attributes every resulting command to whoever (or whatever schedule)
// ran the scene.
//
// Note the 2s per-device rate limit in CommandService: two actions targeting the SAME device inside
// two seconds will see the second refused. That is surfaced in the result rather than hidden — a
// scene should not command one device twice in quick succession, and the report says so if it does.
func (s *SceneService) Run(ctx context.Context, sceneId int64, actor int64, actorName string) (*SceneRunResult, error) {
	scene, err := s.scenes.GetById(ctx, "", uint64(sceneId))
	if err != nil || scene == nil {
		return nil, fmt.Errorf("scene not found")
	}
	if !scene.Enabled {
		return nil, fmt.Errorf("this scene is disabled")
	}
	actions, err := s.actionsFor(ctx, sceneId)
	if err != nil {
		return nil, err
	}
	out := s.runActions(ctx, sceneId, actions, actor, actorName)
	s.logf("ran scene %q (%d actions) for %s", scene.Name, len(out.Results), actorName)
	return out, nil
}

// runActions issues each action in order and collects its outcome. It never stops early and never
// rolls back: a physical action cannot be undone, so the honest thing is to run every action and
// report each result. Extracted so the fan-out + partial-failure behaviour is unit-testable without
// a database.
func (s *SceneService) runActions(ctx context.Context, sceneId int64, actions []*entities.SceneAction, actor int64, actorName string) *SceneRunResult {
	out := &SceneRunResult{SceneId: sceneId, Results: make([]ActionResult, 0, len(actions))}
	for _, a := range actions {
		res := ActionResult{DeviceId: a.DeviceId, CommandName: a.CommandName, Value: a.Value}
		cmd, cerr := s.commands.Issue(ctx, a.DeviceId, IssueRequest{Name: a.CommandName, Value: a.Value}, actor, actorName)
		if cmd != nil {
			res.Status = cmd.Status
			res.Error = cmd.Error
		}
		if cerr != nil && res.Error == "" {
			res.Error = cerr.Error()
			res.Status = "failed"
		}
		out.Results = append(out.Results, res)
	}
	return out
}
