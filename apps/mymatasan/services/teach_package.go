package services

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/atrest"
)

// A .mmskill package is a passphrase-encrypted zip that carries a taught skill
// between mymatasan nodes: manifest + trained weights + the labeled sample
// images (so the receiving node can re-check accuracy locally, and optionally
// retrain/merge). The ROI is intentionally NOT applied on import — zones are
// camera-specific — but the rule template (threshold, cooldown) travels.
const (
	teachPackageFormat      = 1
	teachPackageMinPass     = 8
	teachPackageManifest    = "manifest.json"
	teachPackageWeightsPath = "model/best.pt"
	teachPackageImagesDir   = "images/"
	maxTeachPackageBytes    = 128 * 1024 * 1024
)

// TeachSkillManifest is the plaintext description of a package (also what a
// preview returns before import).
type TeachSkillManifest struct {
	Format             int      `json:"format"`
	AppVersion         string   `json:"appVersion"`
	Name               string   `json:"name"`
	SkillType          string   `json:"skillType"`
	Classes            []string `json:"classes"`
	SuggestedThreshold float64  `json:"suggestedThreshold"`
	Accuracy           float64  `json:"accuracy"`
	Map50              float64  `json:"map50"`
	BaseModel          string   `json:"baseModel"`
	ImageCount         int      `json:"imageCount"`
	ModelIncluded      bool     `json:"modelIncluded"`
	RuleCooldownSecs   int      `json:"ruleCooldownSecs"`
	RuleMinFrames      int      `json:"ruleMinFrames"`
	// AnomalyThreshold travels with anomaly skills (the calibrated raw score
	// stored inside the bank file; kept here too so previews can show it).
	AnomalyThreshold float64 `json:"anomalyThreshold,omitempty"`
}

// teachPackageImageMeta rides beside each bundled image so the receiver can
// restore its annotation without re-boxing.
type teachPackageImageMeta struct {
	Annotations []TrainingAnnotation `json:"annotations"`
}

// ExportSkill builds a passphrase-encrypted .mmskill for the skill. The skill
// must have a trained model (run the accuracy check first). includeImages
// bundles the labeled samples so the target can re-evaluate and retrain.
func (s *teachService) ExportSkill(ctx context.Context, skillId uint64, passphrase string, includeImages bool) (string, []byte, error) {
	if len(strings.TrimSpace(passphrase)) < teachPackageMinPass {
		return "", nil, fmt.Errorf("passphrase must be at least %d characters", teachPackageMinPass)
	}
	skill, err := s.skills.GetById(ctx, "", skillId)
	if err != nil || skill == nil {
		return "", nil, errors.New("skill not found")
	}
	var cfg teachSkillConfig
	if strings.TrimSpace(skill.Config) != "" {
		_ = json.Unmarshal([]byte(skill.Config), &cfg)
	}
	manifest := TeachSkillManifest{
		Format:           teachPackageFormat,
		AppVersion:       s.appVersion,
		Name:             skill.Name,
		SkillType:        skill.SkillType,
		Classes:          teachSkillClasses(skill),
		ModelIncluded:    true,
		RuleCooldownSecs: teachRuleCooldownSeconds,
		RuleMinFrames:    teachRuleMinFrames,
	}
	// The "model" payload differs by kind: YOLO weights for object/inspect,
	// the fitted normal-memory bank for anomaly skills.
	var weights []byte
	if skill.SkillType == TeachSkillTypeAnomaly {
		acfg, aerr := s.anomalyModelReady(skill)
		if aerr != nil {
			return "", nil, aerr
		}
		weights, err = os.ReadFile(acfg.AnomalyModel)
		if err != nil {
			return "", nil, fmt.Errorf("could not read the learned model: %w", err)
		}
		manifest.AnomalyThreshold = acfg.AnomalyThreshold
	} else {
		model, merr := s.skillCandidateModel(ctx, skill)
		if merr != nil {
			return "", nil, merr
		}
		weights, err = os.ReadFile(model.FilePath)
		if err != nil {
			return "", nil, fmt.Errorf("could not read model weights: %w", err)
		}
		manifest.BaseModel = model.BaseModel
	}
	if cfg.Evaluation != nil {
		manifest.SuggestedThreshold = cfg.Evaluation.SuggestedThreshold
		manifest.Accuracy = cfg.Evaluation.Accuracy
		manifest.Map50 = cfg.Evaluation.Map50
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeFile := func(name string, data []byte) error {
		w, werr := zw.Create(name)
		if werr != nil {
			return werr
		}
		_, werr = w.Write(data)
		return werr
	}

	if err := writeFile(teachPackageWeightsPath, weights); err != nil {
		return "", nil, err
	}

	if includeImages && skill.DatasetId > 0 && s.training != nil {
		images, lerr := s.training.ListImages(ctx, skill.DatasetId)
		if lerr == nil {
			for _, img := range images {
				anns := parseAnnotationsJSON(img.Annotations)
				if len(anns) == 0 {
					continue
				}
				data, gerr := s.training.GetImageBytes(ctx, uint64(img.Id))
				if gerr != nil {
					continue
				}
				base := fmt.Sprintf("%s%d", teachPackageImagesDir, img.Id)
				if err := writeFile(base+".jpg", data); err != nil {
					return "", nil, err
				}
				meta, _ := json.Marshal(teachPackageImageMeta{Annotations: anns})
				if err := writeFile(base+".json", meta); err != nil {
					return "", nil, err
				}
				manifest.ImageCount++
			}
		}
	}

	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
	if err := writeFile(teachPackageManifest, manifestJSON); err != nil {
		return "", nil, err
	}
	if err := zw.Close(); err != nil {
		return "", nil, err
	}

	sealed, err := atrest.EncryptWithPassphrase(passphrase, buf.Bytes())
	if err != nil {
		return "", nil, err
	}
	filename := fmt.Sprintf("%s.mmskill", teachClassSlug(skill.Name))
	if filename == ".mmskill" {
		filename = "skill.mmskill"
	}
	return filename, sealed, nil
}

// openTeachPackage decrypts + unzips a package into an in-memory map. Shared by
// preview and import.
func openTeachPackage(sealed []byte, passphrase string) (*TeachSkillManifest, map[string][]byte, error) {
	if len(sealed) == 0 {
		return nil, nil, errors.New("package file is empty")
	}
	plain, err := atrest.DecryptWithPassphrase(passphrase, sealed)
	if err != nil {
		return nil, nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(plain), int64(len(plain)))
	if err != nil {
		return nil, nil, errors.New("package is corrupt or not a .mmskill file")
	}
	files := map[string][]byte{}
	var total int64
	for _, f := range zr.File {
		rc, oerr := f.Open()
		if oerr != nil {
			return nil, nil, oerr
		}
		data, rerr := io.ReadAll(io.LimitReader(rc, maxTeachPackageBytes))
		rc.Close()
		if rerr != nil {
			return nil, nil, rerr
		}
		total += int64(len(data))
		if total > maxTeachPackageBytes {
			return nil, nil, errors.New("package exceeds size limit")
		}
		files[f.Name] = data
	}
	manifestRaw, ok := files[teachPackageManifest]
	if !ok {
		return nil, nil, errors.New("package is missing its manifest")
	}
	var manifest TeachSkillManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return nil, nil, errors.New("package manifest is unreadable")
	}
	if manifest.Format > teachPackageFormat {
		return nil, nil, errors.New("this package was made by a newer version — update the app first")
	}
	return &manifest, files, nil
}

// PreviewSkillPackage decrypts only enough to describe the package (name,
// classes, accuracy, sample count) so the import UI can confirm before commit.
func (s *teachService) PreviewSkillPackage(ctx context.Context, sealed []byte, passphrase string) (*TeachSkillManifest, error) {
	manifest, _, err := openTeachPackage(sealed, passphrase)
	return manifest, err
}

// ImportSkill creates a new, ready-to-activate skill from a package: it
// validates the class names against the local label catalog, recreates the
// dataset + sample images, imports the model weights, and links them. The new
// skill lands at the activation step with NO camera/ROI (those are chosen
// locally) so the operator picks a camera, optionally test-drives, and turns
// it on with the existing flow.
func (s *teachService) ImportSkill(ctx context.Context, sealed []byte, passphrase string, userId int64) (*entities.TeachSkill, error) {
	manifest, files, err := openTeachPackage(sealed, passphrase)
	if err != nil {
		return nil, err
	}
	if s.training == nil {
		return nil, errors.New("training backend unavailable")
	}
	// Guard the same collision the create path guards: a class name matching a
	// stock label would be swallowed by the worker's merge.
	if err := s.validateName(ctx, manifest.Name); err != nil {
		return nil, err
	}
	weights, ok := files[teachPackageWeightsPath]
	if !ok || len(weights) == 0 {
		return nil, errors.New("package has no model weights")
	}

	classes := manifest.Classes
	if len(classes) == 0 {
		classes = []string{teachClassSlug(manifest.Name)}
	}
	now := s.now().UTC().Unix()
	skill := entities.TeachSkill{
		Name:      strings.TrimSpace(manifest.Name),
		SkillType: manifest.SkillType,
		Status:    TeachStatusTrained,
		Step:      teachStepAccuracy, // lands past collection; operator activates
		CreatedBy: userId,
		CreatedAt: now,
		UpdatedBy: userId,
		UpdatedAt: now,
	}

	// Recreate the dataset + sample images so the skill can be re-checked and
	// retrained locally (weights-only import can still activate, but merging
	// with other local skills later needs the samples).
	dataset, err := s.training.SaveDataset(ctx, TrainingDatasetRequest{
		Name:        "Skill: " + skill.Name,
		Description: "Imported via .mmskill",
		Classes:     classes,
	}, userId)
	if err != nil {
		return nil, err
	}
	skill.DatasetId = dataset.Id
	for name, data := range files {
		if !strings.HasPrefix(name, teachPackageImagesDir) || !strings.HasSuffix(name, ".jpg") {
			continue
		}
		metaRaw, ok := files[strings.TrimSuffix(name, ".jpg")+".json"]
		if !ok {
			continue
		}
		var meta teachPackageImageMeta
		if json.Unmarshal(metaRaw, &meta) != nil || len(meta.Annotations) == 0 {
			continue
		}
		_, _ = s.training.StoreCapture(ctx, dataset.Id, data, meta.Annotations, userId)
	}

	// Seed the evaluation config so activation uses the calibrated threshold
	// (the receiver can re-run the check to refresh it against local samples).
	cfg := teachSkillConfig{Evaluation: &TeachEvalReport{
		Accuracy:           manifest.Accuracy,
		Map50:              manifest.Map50,
		SuggestedThreshold: manifest.SuggestedThreshold,
		CreatedAt:          now,
	}}

	if manifest.SkillType == TeachSkillTypeAnomaly {
		// Anomaly model: the row must exist first (its id names the bank file).
		id, cerr := s.skills.Create(ctx, "", skill)
		if cerr != nil {
			return nil, cerr
		}
		skill.Id = int64(id)
		modelPath, perr := s.anomalyModelStorePath(skill.Id)
		if perr != nil {
			return nil, perr
		}
		if werr := os.WriteFile(modelPath, weights, 0o644); werr != nil {
			return nil, fmt.Errorf("could not store the learned model: %w", werr)
		}
		cfg.AnomalyModel = modelPath
		cfg.AnomalyThreshold = manifest.AnomalyThreshold
		if encoded, jerr := json.Marshal(cfg); jerr == nil {
			skill.Config = string(encoded)
		}
		if _, uerr := s.skills.UpdateById(ctx, "", skill); uerr != nil {
			return nil, uerr
		}
		return &skill, nil
	}

	// YOLO skill: import the trained weights as a registry model and link it.
	model, err := s.training.ImportModel(ctx, ImportModelRequest{
		Name:      "Skill: " + skill.Name,
		Classes:   classes,
		BaseModel: manifest.BaseModel,
	}, weights, userId)
	if err != nil {
		return nil, fmt.Errorf("could not import model weights: %w", err)
	}
	skill.ModelId = model.Id
	cfg.Evaluation.ModelId = model.Id
	if encoded, jerr := json.Marshal(cfg); jerr == nil {
		skill.Config = string(encoded)
	}

	id, err := s.skills.Create(ctx, "", skill)
	if err != nil {
		return nil, err
	}
	skill.Id = int64(id)
	return &skill, nil
}
