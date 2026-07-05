package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
)

// Accuracy-check ("pop quiz") tuning. The quick train deliberately trades a few
// points of quality for turnaround — its job is a trustworthy estimate, not the
// final model. The export's val split is the holdout: training never learns
// from it, so predictions on it are an honest test.
const (
	teachQuickEpochsGPU  = 40
	teachQuickEpochsCPU  = 15
	teachQuickImgsz      = 512
	teachEvalMinPerClass = 5
	// A holdout prediction counts as correct when the expected class is seen
	// with at least this confidence.
	teachEvalCorrectConf = 0.25
)

// TeachEvalMiss is one holdout sample the model got wrong — surfaced as the
// "ones that fooled me" gallery.
type TeachEvalMiss struct {
	ImageId    int64   `json:"imageId"`
	Expected   string  `json:"expected"`
	Got        string  `json:"got"` // "" = saw nothing
	Confidence float64 `json:"confidence"`
}

// TeachEvalPerClass is the per-class holdout scoreline.
type TeachEvalPerClass struct {
	Class   string `json:"class"`
	Total   int    `json:"total"`
	Correct int    `json:"correct"`
}

// TeachEvalReport is the persisted result of one accuracy check.
type TeachEvalReport struct {
	Total    int                 `json:"total"`
	Correct  int                 `json:"correct"`
	Accuracy float64             `json:"accuracy"`
	PerClass []TeachEvalPerClass `json:"perClass"`
	Misses   []TeachEvalMiss     `json:"misses"`
	Map50    float64             `json:"map50"`
	// SuggestedThreshold is the alert threshold the auto-created rule should
	// use: comfortably below the model's typical true-detection confidence on
	// the holdout, clamped to a sane band.
	SuggestedThreshold float64 `json:"suggestedThreshold"`
	ModelId            int64   `json:"modelId"`
	CreatedAt          int64   `json:"createdAt"`
}

// TeachEvalState is the wizard's poll payload for the accuracy check.
type TeachEvalState struct {
	Phase    string           `json:"phase"` // none | training | evaluating | done | failed
	Progress int              `json:"progress"`
	Error    string           `json:"error,omitempty"`
	Report   *TeachEvalReport `json:"report,omitempty"`
}

// teachEvalRuntime tracks the single in-flight accuracy check.
type teachEvalRuntime struct {
	skillId  int64
	phase    string
	progress int
	err      string
}

// teachSkillConfig is the skill's Config JSON shape (spare blob on the row).
type teachSkillConfig struct {
	Evaluation *TeachEvalReport `json:"evaluation,omitempty"`
	// RuleId is the auto-created detection rule while the skill is active.
	RuleId int64 `json:"ruleId,omitempty"`
	// AnomalyModel/AnomalyThreshold describe an anomaly skill's fitted memory
	// bank (file path + calibrated raw-score threshold). Anomaly models live
	// outside the custom-YOLO slot, so they're tracked here, not the registry.
	AnomalyModel     string  `json:"anomalyModel,omitempty"`
	AnomalyThreshold float64 `json:"anomalyThreshold,omitempty"`
}

func (s *teachService) StartEvaluation(ctx context.Context, skillId uint64, userId int64) (*TeachEvalState, error) {
	skill, err := s.skills.GetById(ctx, "", skillId)
	if err != nil {
		return nil, err
	}
	if skill == nil {
		return nil, errors.New("skill not found")
	}
	if skill.DatasetId == 0 {
		return nil, errors.New("no samples yet — run a teaching session first")
	}
	s.sessMu.Lock()
	sessionActive := s.runtime != nil
	s.sessMu.Unlock()
	if sessionActive {
		return nil, errors.New("stop the teaching session first — the check trains on a stable set of samples")
	}
	// A running test drive holds the previous check's weights open (Windows
	// file lock) and the GPU; shut it down before retraining.
	s.driveMu.Lock()
	s.stopDriveLocked()
	s.driveMu.Unlock()
	if s.training == nil {
		return nil, errors.New("training backend unavailable")
	}
	capability := s.training.MachineCapability(ctx)
	if !capability.Available {
		return nil, errors.New("this computer cannot train yet — install the AI runtime in Settings → AI first")
	}

	// Every class needs a floor of samples or the split can't test anything.
	classes := teachSkillClasses(skill)
	counts, err := s.classCounts(ctx, skill, classes)
	if err != nil {
		return nil, err
	}
	for _, class := range classes {
		if counts[class] < teachEvalMinPerClass {
			return nil, fmt.Errorf("not enough samples of %q yet (%d of at least %d) — show a few more first", class, counts[class], teachEvalMinPerClass)
		}
	}

	s.evalMu.Lock()
	if s.evalRt != nil {
		s.evalMu.Unlock()
		return nil, errors.New("an accuracy check is already running")
	}
	s.evalRt = &teachEvalRuntime{skillId: skill.Id, phase: "training"}
	s.evalMu.Unlock()
	clearRuntime := func() {
		s.evalMu.Lock()
		s.evalRt = nil
		s.evalMu.Unlock()
	}

	// Anomaly skills don't train a YOLO model — they fit a normal-only memory
	// bank and calibrate its threshold (see teach_anomaly.go).
	if skill.SkillType == TeachSkillTypeAnomaly {
		if skill.Step < teachStepAccuracy {
			skill.Step = teachStepAccuracy
			skill.UpdatedBy = userId
			skill.UpdatedAt = s.now().UTC().Unix()
			if _, err := s.skills.UpdateById(ctx, "", *skill); err != nil {
				clearRuntime()
				return nil, err
			}
		}
		go s.startAnomalyCheck(skill, userId)
		return s.EvaluationState(ctx, skillId)
	}

	// Each check trains a fresh quick model; drop the previous check's model so
	// they don't pile up in the registry (never an active one).
	if skill.ModelId > 0 {
		if old, oerr := s.training.GetModel(ctx, uint64(skill.ModelId)); oerr == nil && old != nil && !old.IsActive && old.Status != "running" && old.Status != "pending" {
			_, _ = s.training.DeleteModel(ctx, uint64(old.Id))
		}
	}

	epochs := teachQuickEpochsCPU
	if capability.Cuda {
		epochs = teachQuickEpochsGPU
	}
	model, err := s.training.StartTraining(ctx, StartTrainingRequest{
		DatasetId: skill.DatasetId,
		Name:      "Skill: " + skill.Name,
		Epochs:    epochs,
		Imgsz:     teachQuickImgsz,
	}, userId)
	if err != nil {
		clearRuntime()
		return nil, err
	}

	skill.ModelId = model.Id
	if skill.Step < teachStepAccuracy {
		skill.Step = teachStepAccuracy
	}
	// A new check invalidates the previous report.
	skill.Config = ""
	skill.UpdatedBy = userId
	skill.UpdatedAt = s.now().UTC().Unix()
	if _, err := s.skills.UpdateById(ctx, "", *skill); err != nil {
		clearRuntime()
		return nil, err
	}

	go s.watchEvaluation(skill.Id, skill.DatasetId, model.Id, userId)
	return s.EvaluationState(ctx, skillId)
}

// watchEvaluation follows the quick train to completion, then runs the holdout
// pass and persists the report onto the skill.
func (s *teachService) watchEvaluation(skillId, datasetId, modelId, userId int64) {
	defer func() {
		s.evalMu.Lock()
		if s.evalRt != nil && s.evalRt.skillId == skillId && s.evalRt.phase != "failed" {
			s.evalRt = nil
		}
		s.evalMu.Unlock()
	}()
	setPhase := func(phase string, progress int, errMsg string) {
		s.evalMu.Lock()
		if s.evalRt != nil && s.evalRt.skillId == skillId {
			s.evalRt.phase = phase
			s.evalRt.progress = progress
			s.evalRt.err = errMsg
		}
		s.evalMu.Unlock()
	}

	var model *entities.TrainingModel
	for {
		time.Sleep(3 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		m, err := s.training.GetModel(ctx, uint64(modelId))
		cancel()
		if err != nil || m == nil {
			setPhase("failed", 0, "training model disappeared")
			return
		}
		switch m.Status {
		case "completed":
			model = m
		case "failed":
			msg := "training failed"
			var metrics map[string]any
			if json.Unmarshal([]byte(m.Metrics), &metrics) == nil {
				if v, ok := metrics["error"].(string); ok && v != "" {
					msg = v
				}
			}
			setPhase("failed", 0, msg)
			return
		default:
			setPhase("training", m.Progress, "")
			continue
		}
		break
	}

	setPhase("evaluating", 100, "")
	evalCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	predictions, err := s.training.EvaluateHoldout(evalCtx, datasetId, model.FilePath)
	cancel()
	if err != nil {
		setPhase("failed", 100, err.Error())
		return
	}

	report := s.buildEvalReport(predictions, model)
	ctx, cancelSave := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelSave()
	skill, err := s.skills.GetById(ctx, "", uint64(skillId))
	if err != nil || skill == nil {
		setPhase("failed", 100, "skill disappeared while evaluating")
		return
	}
	if encoded, jerr := json.Marshal(teachSkillConfig{Evaluation: report}); jerr == nil {
		skill.Config = string(encoded)
	}
	skill.UpdatedBy = userId
	skill.UpdatedAt = s.now().UTC().Unix()
	_, _ = s.skills.UpdateById(ctx, "", *skill)
}

// buildEvalReport grades holdout predictions against each image's stored
// annotation (its class): correct = expected class seen with enough
// confidence; Got records the top-confidence label for the miss gallery.
func (s *teachService) buildEvalReport(predictions []EvalPrediction, model *entities.TrainingModel) *TeachEvalReport {
	report := &TeachEvalReport{ModelId: model.Id, CreatedAt: s.now().UTC().Unix()}
	perClass := map[string]*TeachEvalPerClass{}
	var correctConfs []float64
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, pred := range predictions {
		imageId, err := strconv.ParseInt(pred.ImageBase, 10, 64)
		if err != nil {
			continue
		}
		img, err := s.training.GetImage(ctx, uint64(imageId))
		if err != nil || img == nil {
			continue
		}
		anns := parseAnnotationsJSON(img.Annotations)
		if len(anns) == 0 {
			continue
		}
		expected := anns[0].ClassName

		entry := perClass[expected]
		if entry == nil {
			entry = &TeachEvalPerClass{Class: expected}
			perClass[expected] = entry
		}
		entry.Total++
		report.Total++

		correct := false
		topLabel, topConf, expectedConf := "", 0.0, 0.0
		for _, det := range pred.Detections {
			if det.Confidence > topConf {
				topLabel, topConf = det.Label, det.Confidence
			}
			if det.Label == expected && det.Confidence > expectedConf {
				expectedConf = det.Confidence
			}
		}
		if expectedConf >= teachEvalCorrectConf {
			correct = true
		}
		if correct {
			entry.Correct++
			report.Correct++
			correctConfs = append(correctConfs, expectedConf)
			continue
		}
		report.Misses = append(report.Misses, TeachEvalMiss{
			ImageId:    imageId,
			Expected:   expected,
			Got:        topLabel,
			Confidence: topConf,
		})
	}

	for _, entry := range perClass {
		report.PerClass = append(report.PerClass, *entry)
	}
	if report.Total > 0 {
		report.Accuracy = float64(report.Correct) / float64(report.Total)
	}
	var metrics map[string]float64
	if json.Unmarshal([]byte(model.Metrics), &metrics) == nil {
		report.Map50 = metrics["map50"]
	}
	report.SuggestedThreshold = suggestThreshold(correctConfs)
	return report
}

// suggestThreshold sits the alert threshold at ~80% of the median holdout
// true-detection confidence, clamped to [0.25, 0.6] — low enough not to miss
// what the model reliably sees, high enough to reject noise. With no correct
// detections there is nothing to calibrate on; fall back to a cautious default.
func suggestThreshold(correctConfs []float64) float64 {
	if len(correctConfs) == 0 {
		return 0.35
	}
	sorted := append([]float64(nil), correctConfs...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	median := sorted[len(sorted)/2]
	threshold := 0.8 * median
	if threshold < 0.25 {
		threshold = 0.25
	}
	if threshold > 0.6 {
		threshold = 0.6
	}
	return threshold
}

func (s *teachService) EvaluationState(ctx context.Context, skillId uint64) (*TeachEvalState, error) {
	skill, err := s.skills.GetById(ctx, "", skillId)
	if err != nil {
		return nil, err
	}
	if skill == nil {
		return nil, errors.New("skill not found")
	}

	s.evalMu.Lock()
	rt := s.evalRt
	if rt != nil && rt.skillId == skill.Id {
		state := &TeachEvalState{Phase: rt.phase, Progress: rt.progress, Error: rt.err}
		// A finished-but-failed runtime stays visible until the next start.
		s.evalMu.Unlock()
		return state, nil
	}
	s.evalMu.Unlock()

	if strings.TrimSpace(skill.Config) != "" {
		var cfg teachSkillConfig
		if json.Unmarshal([]byte(skill.Config), &cfg) == nil && cfg.Evaluation != nil {
			return &TeachEvalState{Phase: "done", Progress: 100, Report: cfg.Evaluation}, nil
		}
	}
	// A model still marked running with no runtime means the app restarted
	// mid-check — surface it as failed so the user can just run it again.
	if skill.ModelId > 0 {
		if m, merr := s.training.GetModel(ctx, uint64(skill.ModelId)); merr == nil && m != nil && (m.Status == "running" || m.Status == "pending") {
			return &TeachEvalState{Phase: "failed", Error: "the check was interrupted — run it again"}, nil
		}
	}
	return &TeachEvalState{Phase: "none"}, nil
}

// classCounts tallies stored samples per class from the skill's dataset.
func (s *teachService) classCounts(ctx context.Context, skill *entities.TeachSkill, classes []string) (map[string]int, error) {
	counts := map[string]int{}
	for _, c := range classes {
		counts[c] = 0
	}
	if skill.DatasetId == 0 || s.training == nil {
		return counts, nil
	}
	images, err := s.training.ListImages(ctx, skill.DatasetId)
	if err != nil {
		return counts, err
	}
	for _, img := range images {
		for _, ann := range parseAnnotationsJSON(img.Annotations) {
			if _, known := counts[ann.ClassName]; known {
				counts[ann.ClassName]++
			}
			break
		}
	}
	return counts, nil
}
