package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/procutil"
	"github.com/mysayasan/kopiv2/infra/vision"
)

// Anomaly skills ("spot anything unusual") learn from good-only samples: the
// fitter embeds every normal frame, keeps the embeddings as a memory bank, and
// calibrates an alert threshold on held-back normal frames. The live worker
// scores each frame against the bank (see yolo_worker.py) and emits a synthetic
// detection with the skill's class when the scene deviates — from there the
// normal rule/alert pipeline takes over. Anomaly models do NOT occupy the
// single custom-YOLO slot, so several can run at once (one per camera each).

// teachAnomalyManifestEntry is one activated anomaly skill in the manifest the
// YOLO worker reads (MYMATASAN_ANOMALY_FILE).
type teachAnomalyManifestEntry struct {
	SkillId   int64      `json:"skillId"`
	CameraId  int64      `json:"cameraId"`
	Label     string     `json:"label"`
	ModelPath string     `json:"modelPath"`
	Roi       [4]float64 `json:"roi"`
}

// anomalyWorkerScript locates anomaly_worker.py next to the resolved detector
// worker script (the .py entry in the detector args).
func (s *teachService) anomalyWorkerScript() (string, error) {
	for _, arg := range s.detectorCfg.Args {
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(arg)), ".py") {
			return filepath.Join(filepath.Dir(arg), "anomaly_worker.py"), nil
		}
	}
	return "", errors.New("detector worker script not configured")
}

// anomalyModelStorePath is the canonical on-disk location for a skill's fitted
// bank: next to the manifest, keyed by skill id.
func (s *teachService) anomalyModelStorePath(skillId int64) (string, error) {
	if strings.TrimSpace(s.detectorCfg.AnomalyManifest) == "" {
		return "", errors.New("anomaly manifest path not configured")
	}
	dir := filepath.Dir(s.detectorCfg.AnomalyManifest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("anomaly_skill_%d.pt", skillId)), nil
}

// startAnomalyCheck is the anomaly branch of the accuracy check: instead of a
// YOLO quick-train it exports the dataset, fits the memory bank on the train
// split, and calibrates the threshold on the val split. The report reads as
// "I tested myself on N held-back normal moments — M looked normal to me".
func (s *teachService) startAnomalyCheck(skill *entities.TeachSkill, userId int64) {
	setPhase := func(phase string, progress int, errMsg string) {
		s.evalMu.Lock()
		if s.evalRt != nil && s.evalRt.skillId == skill.Id {
			s.evalRt.phase = phase
			s.evalRt.progress = progress
			s.evalRt.err = errMsg
		}
		s.evalMu.Unlock()
	}
	defer func() {
		s.evalMu.Lock()
		if s.evalRt != nil && s.evalRt.skillId == skill.Id && s.evalRt.phase != "failed" {
			s.evalRt = nil
		}
		s.evalMu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	script, err := s.anomalyWorkerScript()
	if err != nil {
		setPhase("failed", 0, err.Error())
		return
	}
	outPath, err := s.anomalyModelStorePath(skill.Id)
	if err != nil {
		setPhase("failed", 0, err.Error())
		return
	}
	exportDir, err := s.training.BuildExport(ctx, skill.DatasetId)
	if err != nil {
		setPhase("failed", 0, err.Error())
		return
	}

	cmd := exec.CommandContext(ctx, s.detectorCfg.Command, script,
		"--train", filepath.Join(exportDir, "images", "train"),
		"--calib", filepath.Join(exportDir, "images", "val"),
		"--out", outPath,
	)
	procutil.HideWindow(cmd)
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		setPhase("failed", 0, err.Error())
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		setPhase("failed", 0, err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		setPhase("failed", 0, err.Error())
		return
	}
	go func() {
		sc := bufio.NewScanner(stderrPipe)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			log.Printf("teach-anomaly: %s", sc.Text())
		}
	}()

	type calibScore struct {
		image string
		score float64
	}
	var scores []calibScore
	var threshold float64
	var failMsg string
	completed := false
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var ev struct {
			Event     string  `json:"event"`
			Done      int     `json:"done"`
			Total     int     `json:"total"`
			Image     string  `json:"image"`
			Score     float64 `json:"score"`
			Threshold float64 `json:"threshold"`
			Message   string  `json:"message"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(scanner.Text())), &ev) != nil {
			continue
		}
		switch ev.Event {
		case "progress":
			pct := 0
			if ev.Total > 0 {
				pct = ev.Done * 100 / ev.Total
			}
			setPhase("training", pct, "")
		case "score":
			scores = append(scores, calibScore{image: ev.Image, score: ev.Score})
		case "completed":
			threshold = ev.Threshold
			completed = true
		case "error":
			failMsg = ev.Message
		}
	}
	waitErr := cmd.Wait()
	if failMsg != "" {
		setPhase("failed", 0, failMsg)
		return
	}
	if waitErr != nil || !completed {
		msg := "the learning process failed"
		if waitErr != nil {
			msg = "the learning process failed: " + waitErr.Error()
		}
		setPhase("failed", 0, msg)
		return
	}

	// Report: every held-back normal moment scoring at/below the threshold
	// counts as "looked normal"; the ones above become the false-alarm gallery.
	report := &TeachEvalReport{
		SuggestedThreshold: 0.5, // rule confidence floor; raw threshold lives in the model
		CreatedAt:          s.now().UTC().Unix(),
	}
	slug := teachClassSlug(skill.Name)
	normalClass := "not " + slug
	for _, sc := range scores {
		report.Total++
		if sc.score <= threshold {
			report.Correct++
			continue
		}
		imageId, _ := strconv.ParseInt(sc.image, 10, 64)
		margin := threshold * 0.5
		if margin <= 0 {
			margin = 1e-6
		}
		confidence := 0.5 + 0.49*minFloat(1.0, (sc.score-threshold)/margin)
		report.Misses = append(report.Misses, TeachEvalMiss{
			ImageId:    imageId,
			Expected:   normalClass,
			Got:        slug,
			Confidence: confidence,
		})
	}
	if report.Total > 0 {
		report.Accuracy = float64(report.Correct) / float64(report.Total)
	}
	report.PerClass = []TeachEvalPerClass{{Class: normalClass, Total: report.Total, Correct: report.Correct}}

	saveCtx, cancelSave := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelSave()
	fresh, err := s.skills.GetById(saveCtx, "", uint64(skill.Id))
	if err != nil || fresh == nil {
		setPhase("failed", 100, "skill disappeared while learning")
		return
	}
	var cfg teachSkillConfig
	if strings.TrimSpace(fresh.Config) != "" {
		_ = json.Unmarshal([]byte(fresh.Config), &cfg)
	}
	cfg.Evaluation = report
	cfg.AnomalyModel = outPath
	cfg.AnomalyThreshold = threshold
	if encoded, jerr := json.Marshal(cfg); jerr == nil {
		fresh.Config = string(encoded)
	}
	fresh.UpdatedBy = userId
	fresh.UpdatedAt = s.now().UTC().Unix()
	_, _ = s.skills.UpdateById(saveCtx, "", *fresh)
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// anomalyModelReady loads the skill's config and verifies the fitted bank file
// exists — the anomaly counterpart of skillCandidateModel.
func (s *teachService) anomalyModelReady(skill *entities.TeachSkill) (teachSkillConfig, error) {
	var cfg teachSkillConfig
	if strings.TrimSpace(skill.Config) != "" {
		_ = json.Unmarshal([]byte(skill.Config), &cfg)
	}
	if strings.TrimSpace(cfg.AnomalyModel) == "" {
		return cfg, errors.New("run the accuracy check first — it teaches me what normal looks like")
	}
	if _, err := os.Stat(cfg.AnomalyModel); err != nil {
		return cfg, errors.New("the learned model is missing — run the accuracy check again")
	}
	return cfg, nil
}

// readAnomalyManifest loads the worker manifest (empty list when absent).
func (s *teachService) readAnomalyManifest() []teachAnomalyManifestEntry {
	if strings.TrimSpace(s.detectorCfg.AnomalyManifest) == "" {
		return nil
	}
	raw, err := os.ReadFile(s.detectorCfg.AnomalyManifest)
	if err != nil {
		return nil
	}
	var entries []teachAnomalyManifestEntry
	_ = json.Unmarshal(raw, &entries)
	return entries
}

// writeAnomalyManifest persists the manifest; the worker re-reads it on reload.
func (s *teachService) writeAnomalyManifest(entries []teachAnomalyManifestEntry) error {
	if strings.TrimSpace(s.detectorCfg.AnomalyManifest) == "" {
		return errors.New("anomaly manifest path not configured")
	}
	if err := os.MkdirAll(filepath.Dir(s.detectorCfg.AnomalyManifest), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.detectorCfg.AnomalyManifest, encoded, 0o644)
}

// upsertAnomalyManifestEntry adds/replaces the skill's entry and reloads the
// live worker so it starts (or stops) scoring immediately.
func (s *teachService) upsertAnomalyManifestEntry(skill *entities.TeachSkill, cfg teachSkillConfig) error {
	roi := vision.ZoneBounds(skill.RoiPolygon)
	entry := teachAnomalyManifestEntry{
		SkillId:   skill.Id,
		CameraId:  skill.CameraId,
		Label:     teachClassSlug(skill.Name),
		ModelPath: cfg.AnomalyModel,
		Roi:       [4]float64{roi.X, roi.Y, roi.W, roi.H},
	}
	entries := s.readAnomalyManifest()
	replaced := false
	for i := range entries {
		if entries[i].SkillId == skill.Id {
			entries[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		entries = append(entries, entry)
	}
	if err := s.writeAnomalyManifest(entries); err != nil {
		return err
	}
	s.training.ReloadDetector()
	return nil
}

// removeAnomalyManifestEntry drops the skill's entry (no-op when absent).
func (s *teachService) removeAnomalyManifestEntry(skillId int64) {
	entries := s.readAnomalyManifest()
	kept := entries[:0]
	changed := false
	for _, e := range entries {
		if e.SkillId == skillId {
			changed = true
			continue
		}
		kept = append(kept, e)
	}
	if changed {
		_ = s.writeAnomalyManifest(kept)
		s.training.ReloadDetector()
	}
}
