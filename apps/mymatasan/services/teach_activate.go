package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/vision"
)

// Test-drive tuning: the candidate worker is heavyweight (its own Python +
// model), so it idles out quickly when the wizard stops polling.
const (
	teachDriveIdleTimeout = 90 * time.Second
	teachDriveFrameConf   = 0.2
	// Rule defaults for the auto-created detection rule on activation.
	teachRuleCooldownSeconds = 60
	teachRuleMinFrames       = 1
)

// TeachDetectorConfig is how the test drive launches a second YOLO worker: the
// same command/script as the live detector, but with the candidate weights
// forced via env so the live pipeline is never touched. AnomalyManifest is the
// JSON file activated anomaly skills are registered in (the live worker reads
// the same path via MYMATASAN_ANOMALY_FILE).
type TeachDetectorConfig struct {
	Command         string
	Args            []string
	TimeoutMs       int
	AnomalyManifest string
}

// TeachDriveFrame is one test-drive poll result: the camera frame with the
// candidate model's detections drawn on, plus the raw detections for the
// "Seeing: X (82%)" line. Ready=false while the worker is still warming up.
type TeachDriveFrame struct {
	Ready      bool                  `json:"ready"`
	Image      string                `json:"image,omitempty"` // base64 JPEG, annotated
	Detections []vision.MetaBox      `json:"detections"`
	Error      string                `json:"error,omitempty"`
}

// teachDriveRuntime is the single active test-drive worker.
type teachDriveRuntime struct {
	skillId  int64
	cameraId int64
	detector *vision.PersistentObjectDetector
	ready    bool
	warmErr  string
	lastUsed time.Time
}

// TeachActivateResult is what activation returns: the created rule alongside
// the refreshed skill.
type TeachActivateResult struct {
	Skill *entities.TeachSkill    `json:"skill"`
	Rule  *entities.DetectionRule `json:"rule"`
}

func (s *teachService) skillCandidateModel(ctx context.Context, skill *entities.TeachSkill) (*entities.TrainingModel, error) {
	if skill.ModelId == 0 {
		return nil, errors.New("run the accuracy check first — it trains the model this step needs")
	}
	model, err := s.training.GetModel(ctx, uint64(skill.ModelId))
	if err != nil || model == nil {
		return nil, errors.New("the checked model is missing — run the accuracy check again")
	}
	// "completed" = locally trained (accuracy check); "imported" = brought in via
	// a .mmskill package. Both are ready-to-use weights on disk.
	ready := model.Status == "completed" || model.Status == "imported"
	if !ready || strings.TrimSpace(model.FilePath) == "" {
		return nil, errors.New("the model is not ready yet — finish the accuracy check first")
	}
	return model, nil
}

func (s *teachService) StartTestDrive(ctx context.Context, skillId uint64, userId int64) error {
	skill, err := s.skills.GetById(ctx, "", skillId)
	if err != nil || skill == nil {
		return errors.New("skill not found")
	}
	if skill.CameraId == 0 {
		return errors.New("pick a camera for this skill first")
	}
	// Candidate readiness + the env that makes the second worker score ONLY the
	// candidate: YOLO skills override the stock slot with the checked weights;
	// anomaly skills get a private manifest holding just this skill.
	var env []string
	if skill.SkillType == TeachSkillTypeAnomaly {
		cfg, cerr := s.anomalyModelReady(skill)
		if cerr != nil {
			return cerr
		}
		candidate := filepath.Join(filepath.Dir(cfg.AnomalyModel), fmt.Sprintf("testdrive_skill_%d.json", skill.Id))
		roi := vision.ZoneBounds(skill.RoiPolygon)
		entry := []teachAnomalyManifestEntry{{
			SkillId:   skill.Id,
			CameraId:  skill.CameraId,
			Label:     teachClassSlug(skill.Name),
			ModelPath: cfg.AnomalyModel,
			Roi:       [4]float64{roi.X, roi.Y, roi.W, roi.H},
		}}
		encoded, jerr := json.Marshal(entry)
		if jerr != nil {
			return jerr
		}
		if werr := os.WriteFile(candidate, encoded, 0o644); werr != nil {
			return werr
		}
		env = []string{
			"MYMATASAN_ANOMALY_FILE=" + candidate,
			"MYMATASAN_ACTIVE_MODEL_FILE=" + filepath.Join(filepath.Dir(cfg.AnomalyModel), "no-active-model"),
			"MYMATASAN_LPR_MODEL_FILE=" + filepath.Join(filepath.Dir(cfg.AnomalyModel), "no-lpr-model"),
		}
	} else {
		model, merr := s.skillCandidateModel(ctx, skill)
		if merr != nil {
			return merr
		}
		weights, _ := filepath.Abs(model.FilePath)
		env = []string{
			"MYMATASAN_YOLO_MODEL=" + weights,
			"MYMATASAN_ACTIVE_MODEL_FILE=" + filepath.Join(filepath.Dir(weights), "no-active-model"),
			"MYMATASAN_LPR_MODEL_FILE=" + filepath.Join(filepath.Dir(weights), "no-lpr-model"),
			"MYMATASAN_ANOMALY_FILE=" + filepath.Join(filepath.Dir(weights), "no-anomaly-manifest"),
		}
	}
	if strings.TrimSpace(s.detectorCfg.Command) == "" || len(s.detectorCfg.Args) == 0 {
		return errors.New("the detector worker is not configured on this host")
	}

	s.driveMu.Lock()
	defer s.driveMu.Unlock()
	if s.driveRt != nil {
		if s.driveRt.skillId == skill.Id {
			s.driveRt.lastUsed = time.Now()
			return nil // already running for this skill
		}
		s.stopDriveLocked()
	}

	timeout := time.Duration(s.detectorCfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	detector, err := vision.NewPersistentObjectDetector(vision.PersistentObjectDetectorOptions{
		Command: s.detectorCfg.Command,
		Args:    s.detectorCfg.Args,
		Timeout: timeout,
		Env:     env,
	})
	if err != nil {
		return err
	}
	rt := &teachDriveRuntime{skillId: skill.Id, cameraId: skill.CameraId, detector: detector, lastUsed: time.Now()}
	s.driveRt = rt

	// Warm up in the background: first inference pays worker launch + model
	// load (GPU init can take tens of seconds).
	go func() {
		warmCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		frame, err := s.source.Capture(warmCtx, rt.cameraId)
		if err == nil {
			_, err = detector.WarmupDetect(warmCtx, frame)
		}
		s.driveMu.Lock()
		if s.driveRt == rt {
			rt.ready = err == nil
			if err != nil {
				rt.warmErr = err.Error()
			}
		}
		s.driveMu.Unlock()
	}()

	// Idle reaper: shut the candidate worker down when the wizard stops polling.
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			s.driveMu.Lock()
			if s.driveRt != rt {
				s.driveMu.Unlock()
				return
			}
			if time.Since(rt.lastUsed) > teachDriveIdleTimeout {
				s.stopDriveLocked()
				s.driveMu.Unlock()
				return
			}
			s.driveMu.Unlock()
		}
	}()
	return nil
}

// stopDriveLocked closes the candidate worker; callers hold driveMu.
func (s *teachService) stopDriveLocked() {
	if s.driveRt != nil {
		_ = s.driveRt.detector.Close()
		s.driveRt = nil
	}
}

func (s *teachService) StopTestDrive(ctx context.Context, skillId uint64) {
	s.driveMu.Lock()
	defer s.driveMu.Unlock()
	if s.driveRt != nil && s.driveRt.skillId == int64(skillId) {
		s.stopDriveLocked()
	}
}

// TestDriveFrame grabs a fresh camera frame, runs the candidate model on it,
// and returns the annotated image + detections.
func (s *teachService) TestDriveFrame(ctx context.Context, skillId uint64) (*TeachDriveFrame, error) {
	s.driveMu.Lock()
	rt := s.driveRt
	if rt == nil || rt.skillId != int64(skillId) {
		s.driveMu.Unlock()
		return nil, errors.New("test drive is not running — start it first")
	}
	rt.lastUsed = time.Now()
	ready, warmErr := rt.ready, rt.warmErr
	s.driveMu.Unlock()

	if warmErr != "" {
		return &TeachDriveFrame{Ready: false, Error: warmErr, Detections: []vision.MetaBox{}}, nil
	}
	if !ready {
		return &TeachDriveFrame{Ready: false, Detections: []vision.MetaBox{}}, nil
	}

	frame, err := s.source.Capture(ctx, rt.cameraId)
	if err != nil {
		return &TeachDriveFrame{Ready: true, Error: err.Error(), Detections: []vision.MetaBox{}}, nil
	}
	candidates, err := rt.detector.DetectObjects(ctx, frame)
	if err != nil {
		return &TeachDriveFrame{Ready: true, Error: err.Error(), Detections: []vision.MetaBox{}}, nil
	}

	boxes := make([]vision.MetaBox, 0, len(candidates))
	annotated := make([]vision.AnnotatedBox, 0, len(candidates))
	for _, c := range candidates {
		if c.Confidence < teachDriveFrameConf {
			continue
		}
		boxes = append(boxes, vision.MetaBox{Box: c.Box, Label: c.Label, Confidence: c.Confidence})
		annotated = append(annotated, vision.AnnotatedBox{
			Box:   c.Box,
			Label: fmt.Sprintf("%s %d%%", c.Label, int(c.Confidence*100)),
		})
	}
	image := frame.Data
	if len(annotated) > 0 {
		if drawn := vision.AnnotateJPEG(image, annotated, 85); len(drawn) > 0 {
			image = drawn
		}
	}
	return &TeachDriveFrame{
		Ready:      true,
		Image:      base64.StdEncoding.EncodeToString(image),
		Detections: boxes,
	}, nil
}

// ActivateSkill turns the skill on for real: hot-swap the checked model in as
// the custom detector, auto-create the detection rule (camera + taught class +
// ROI + calibrated threshold), and mark the skill active.
func (s *teachService) ActivateSkill(ctx context.Context, skillId uint64, userId int64) (*TeachActivateResult, error) {
	skill, err := s.skills.GetById(ctx, "", skillId)
	if err != nil || skill == nil {
		return nil, errors.New("skill not found")
	}
	if skill.CameraId == 0 {
		return nil, errors.New("pick a camera for this skill first")
	}
	if s.vision == nil {
		return nil, errors.New("detection rules service unavailable")
	}

	// The test-drive worker holds the GPU; release it before going live.
	s.driveMu.Lock()
	s.stopDriveLocked()
	s.driveMu.Unlock()

	var cfg teachSkillConfig
	if strings.TrimSpace(skill.Config) != "" {
		_ = json.Unmarshal([]byte(skill.Config), &cfg)
	}
	threshold := 0.35
	if cfg.Evaluation != nil && cfg.Evaluation.SuggestedThreshold > 0 {
		threshold = cfg.Evaluation.SuggestedThreshold
	}

	slug := teachClassSlug(skill.Name)
	if skill.SkillType == TeachSkillTypeAnomaly {
		// Anomaly skills go live via the worker manifest (own slot per camera)
		// instead of the single custom-YOLO model.
		acfg, aerr := s.anomalyModelReady(skill)
		if aerr != nil {
			return nil, aerr
		}
		cfg.AnomalyModel, cfg.AnomalyThreshold = acfg.AnomalyModel, acfg.AnomalyThreshold
		if err := s.upsertAnomalyManifestEntry(skill, cfg); err != nil {
			return nil, err
		}
		// Register the class so the rule's Detect target resolves.
		if s.classes != nil {
			_ = s.classes.UpsertTrained(ctx, []string{slug}, userId)
		}
		threshold = 0.5 // worker maps "at the calibrated threshold" to 0.5
	} else {
		model, merr := s.skillCandidateModel(ctx, skill)
		if merr != nil {
			return nil, merr
		}
		if _, err := s.training.ActivateModel(ctx, uint64(model.Id), userId); err != nil {
			return nil, err
		}
	}
	rule, err := s.vision.SaveRule(ctx, DetectionRuleRequest{
		Id:              cfg.RuleId, // re-activate updates the existing rule instead of duplicating
		CameraId:        skill.CameraId,
		Name:            skill.Name,
		DetectionType:   slug,
		ZonePolygon:     skill.RoiPolygon,
		Threshold:       threshold,
		MinFrames:       teachRuleMinFrames,
		CooldownSeconds: teachRuleCooldownSeconds,
		IsEnabled:       true,
	}, userId)
	if err != nil {
		return nil, fmt.Errorf("model activated but rule creation failed: %w", err)
	}

	cfg.RuleId = rule.Id
	if encoded, jerr := json.Marshal(cfg); jerr == nil {
		skill.Config = string(encoded)
	}
	skill.Status = TeachStatusActive
	skill.Step = teachMaxStep
	skill.UpdatedBy = userId
	skill.UpdatedAt = s.now().UTC().Unix()
	if _, err := s.skills.UpdateById(ctx, "", *skill); err != nil {
		return nil, err
	}
	return &TeachActivateResult{Skill: skill, Rule: rule}, nil
}

// DeactivateSkill turns the skill off: delete its rule, revert detection to
// the stock model (when this skill's model is the active one), and mark the
// skill back as trained. The model and samples stay for a later re-activate.
func (s *teachService) DeactivateSkill(ctx context.Context, skillId uint64, userId int64) (*entities.TeachSkill, error) {
	skill, err := s.skills.GetById(ctx, "", skillId)
	if err != nil || skill == nil {
		return nil, errors.New("skill not found")
	}

	var cfg teachSkillConfig
	if strings.TrimSpace(skill.Config) != "" {
		_ = json.Unmarshal([]byte(skill.Config), &cfg)
	}
	if cfg.RuleId > 0 && s.vision != nil {
		_, _ = s.vision.DeleteRule(ctx, uint64(cfg.RuleId))
		cfg.RuleId = 0
	}
	if skill.SkillType == TeachSkillTypeAnomaly {
		// Anomaly skills leave via the manifest; the fitted model stays on disk
		// for a later re-activate.
		s.removeAnomalyManifestEntry(skill.Id)
	} else if skill.ModelId > 0 {
		// Only step off the custom-model slot if it is still this skill's model —
		// another skill (or a manual import) may have taken it since.
		if m, merr := s.training.GetModel(ctx, uint64(skill.ModelId)); merr == nil && m != nil && m.IsActive {
			_ = s.training.DeactivateModel(ctx, userId)
		}
	}

	if encoded, jerr := json.Marshal(cfg); jerr == nil {
		skill.Config = string(encoded)
	}
	skill.Status = TeachStatusTrained
	skill.UpdatedBy = userId
	skill.UpdatedAt = s.now().UTC().Unix()
	if _, err := s.skills.UpdateById(ctx, "", *skill); err != nil {
		return nil, err
	}
	return skill, nil
}
