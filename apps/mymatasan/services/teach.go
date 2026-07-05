package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// Teach skill types and lifecycle statuses. T1 ships the wizard shell (draft
// skills through the ROI step); collecting/trained/active arrive with the
// capture and training phases.
const (
	TeachSkillTypeObject  = "object"  // recognize a new object anywhere in view
	TeachSkillTypeInspect = "inspect" // tell good from bad in a fixed spot
	TeachSkillTypeAnomaly = "anomaly" // learn normal, alert on deviation (good-only samples)

	TeachStatusDraft      = "draft"
	TeachStatusCollecting = "collecting"
	TeachStatusTrained    = "trained"
	TeachStatusActive     = "active"

	// Wizard steps stored on the skill so the wizard can resume where the user
	// left off: 1=name, 2=skill type, 3=camera+ROI, 4=sample collection,
	// 5=accuracy check, 6=activation.
	teachMinStep      = 1
	teachStepCollect  = 4
	teachStepAccuracy = 5
	teachMaxStep      = 6
)

// TeachSkillRequest is the create/update shape for a wizard skill. Zero/empty
// fields are "no change" on update (Step only moves forward via the wizard).
type TeachSkillRequest struct {
	Name       string `json:"name"`
	SkillType  string `json:"skillType"`
	CameraId   int64  `json:"cameraId"`
	RoiPolygon string `json:"roiPolygon"`
	Step       int    `json:"step"`
}

type teachService struct {
	skills      dbsql.IGenericRepo[entities.TeachSkill]
	classes     IDetectionClassService
	training    ITrainingService
	vision      IVisionService
	source      *DetectionSource
	detectorCfg TeachDetectorConfig
	appVersion  string
	// snapshotCipher decrypts alert snapshots so feedback can re-file them into
	// the dataset (nil = plaintext snapshots).
	snapshotCipher *atrest.Cipher
	now            func() time.Time

	// One teaching session runs at a time (one camera being shown examples);
	// runtime is nil when idle. See teach_capture.go for the engine.
	sessMu  sync.Mutex
	runtime *teachRuntime

	// One accuracy check ("pop quiz") runs at a time; see teach_eval.go.
	evalMu sync.Mutex
	evalRt *teachEvalRuntime

	// One test-drive worker runs at a time; see teach_activate.go.
	driveMu sync.Mutex
	driveRt *teachDriveRuntime
}

// NewTeachService creates the Teach wizard's skill registry + capture engine.
// classes provides the label catalog used to reject skill names that collide
// with a label the detector already emits (the worker's merge would dedupe
// same-label boxes across models, silently swallowing the taught class);
// training stores captured session frames and trains/activates models; vision
// manages the auto-created detection rule; source grabs camera frames;
// detectorCfg launches the test-drive worker.
func NewTeachService(
	skills dbsql.IGenericRepo[entities.TeachSkill],
	classes IDetectionClassService,
	training ITrainingService,
	visionServ IVisionService,
	source *DetectionSource,
	detectorCfg TeachDetectorConfig,
	appVersion string,
	snapshotCipher *atrest.Cipher,
) ITeachService {
	return &teachService{
		skills:         skills,
		classes:        classes,
		training:       training,
		vision:         visionServ,
		source:         source,
		detectorCfg:    detectorCfg,
		appVersion:     appVersion,
		snapshotCipher: snapshotCipher,
		now:            time.Now,
	}
}

// teachClassSlug canonicalizes a skill name into the class label it will teach:
// lowercase with single spaces — NEVER underscores, matching the exact-match
// convention of raw detector labels.
func teachClassSlug(name string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(name))), " ")
}

// validateName rejects empty names and names whose class slug collides with a
// stock label (e.g. teaching "person"): the stock model already emits it, so the
// taught class could never be told apart from stock detections.
func (s *teachService) validateName(ctx context.Context, name string) error {
	slug := teachClassSlug(name)
	if slug == "" {
		return errors.New("skill name is required")
	}
	if s.classes == nil {
		return nil
	}
	catalog, err := s.classes.LabelCatalog(ctx)
	if err != nil {
		return nil // catalog is best-effort; create/update must not break on it
	}
	for _, entry := range catalog {
		if entry.Source == "stock" && entry.Label == slug {
			return fmt.Errorf("%q is already a built-in detection label — pick a more specific name (e.g. %q)", slug, "my "+slug)
		}
	}
	return nil
}

func validSkillType(value string) bool {
	return value == TeachSkillTypeObject || value == TeachSkillTypeInspect || value == TeachSkillTypeAnomaly
}

func (s *teachService) ListSkills(ctx context.Context) ([]*entities.TeachSkill, error) {
	rows, _, err := s.skills.Get(ctx, "", 1000, 0, nil, []sqldataenums.Sorter{{FieldName: "UpdatedAt", Sort: sqldataenums.DESC}})
	return rows, err
}

func (s *teachService) GetSkill(ctx context.Context, id uint64) (*entities.TeachSkill, error) {
	return s.skills.GetById(ctx, "", id)
}

func (s *teachService) CreateSkill(ctx context.Context, req TeachSkillRequest, userId int64) (*entities.TeachSkill, error) {
	if err := s.validateName(ctx, req.Name); err != nil {
		return nil, err
	}
	skillType := req.SkillType
	if skillType != "" && !validSkillType(skillType) {
		return nil, errors.New("unknown skill type")
	}
	now := s.now().UTC().Unix()
	row := entities.TeachSkill{
		CameraId:  req.CameraId,
		Name:      strings.TrimSpace(req.Name),
		SkillType: skillType,
		Status:    TeachStatusDraft,
		Step:      teachMinStep,
		CreatedBy: userId,
		CreatedAt: now,
		UpdatedBy: userId,
		UpdatedAt: now,
	}
	if req.Step > teachMinStep && req.Step <= teachMaxStep {
		row.Step = req.Step
	}
	id, err := s.skills.Create(ctx, "", row)
	if err != nil {
		return nil, err
	}
	row.Id = int64(id)
	return &row, nil
}

func (s *teachService) UpdateSkill(ctx context.Context, id uint64, req TeachSkillRequest, userId int64) (*entities.TeachSkill, error) {
	row, err := s.skills.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errors.New("skill not found")
	}
	if req.Name != "" && strings.TrimSpace(req.Name) != row.Name {
		if err := s.validateName(ctx, req.Name); err != nil {
			return nil, err
		}
		row.Name = strings.TrimSpace(req.Name)
	}
	if req.SkillType != "" {
		if !validSkillType(req.SkillType) {
			return nil, errors.New("unknown skill type")
		}
		row.SkillType = req.SkillType
	}
	if req.CameraId != 0 {
		row.CameraId = req.CameraId
	}
	if req.RoiPolygon != "" {
		row.RoiPolygon = req.RoiPolygon
	}
	if req.Step >= teachMinStep && req.Step <= teachMaxStep {
		row.Step = req.Step
	}
	row.UpdatedBy = userId
	row.UpdatedAt = s.now().UTC().Unix()
	if _, err := s.skills.UpdateById(ctx, "", *row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *teachService) DeleteSkill(ctx context.Context, id uint64) (uint64, error) {
	// Stop an active session on this skill and drop its backing dataset so
	// deleting a skill never leaves an orphaned auto-created dataset behind.
	s.sessMu.Lock()
	rt := s.runtime
	s.sessMu.Unlock()
	if rt != nil && rt.skillId == int64(id) {
		rt.cancel()
		select {
		case <-rt.done:
		case <-time.After(3 * time.Second):
		}
	}
	s.driveMu.Lock()
	if s.driveRt != nil && s.driveRt.skillId == int64(id) {
		s.stopDriveLocked()
	}
	s.driveMu.Unlock()

	skill, err := s.skills.GetById(ctx, "", id)
	if err == nil && skill != nil {
		// Forget = fully undo: rule gone, detection back on stock, then the
		// skill's model, dataset, and row.
		if skill.Status == TeachStatusActive {
			_, _ = s.DeactivateSkill(ctx, id, skill.UpdatedBy)
		}
		if skill.SkillType == TeachSkillTypeAnomaly {
			s.removeAnomalyManifestEntry(skill.Id)
			if cfg, cerr := s.anomalyModelReady(skill); cerr == nil {
				_ = os.Remove(cfg.AnomalyModel)
			}
		}
		if skill.ModelId > 0 && s.training != nil {
			if m, merr := s.training.GetModel(ctx, uint64(skill.ModelId)); merr == nil && m != nil && !m.IsActive {
				_, _ = s.training.DeleteModel(ctx, uint64(m.Id))
			}
		}
		if skill.DatasetId > 0 && s.training != nil {
			_, _ = s.training.DeleteDataset(ctx, uint64(skill.DatasetId))
		}
	}
	return s.skills.DeleteById(ctx, "", id)
}
