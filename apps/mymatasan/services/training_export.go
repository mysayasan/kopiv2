package services

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// modelTable is passed explicitly to the model repo because the generic repo
// derives table names by stripping a trailing "_model" from the type name
// (a view-model convention) — which would turn TrainingModel into the
// non-existent "training" table. The bootstrap creates it as "training_model".
const modelTable = "training_model"

// ImportModelRequest is the metadata for importing externally-trained weights.
type ImportModelRequest struct {
	Name      string   `json:"name"`
	Classes   []string `json:"classes"`
	BaseModel string   `json:"baseModel"`
}

// ExportZip materializes the dataset as a YOLO dataset directory and zips it,
// returning the zip path. This is the dataset you train on (in app, or offline)
// to produce a best.pt.
func (s *trainingService) ExportZip(ctx context.Context, datasetId int64) (string, error) {
	exportDir, names, err := s.buildExportDir(ctx, datasetId)
	if err != nil {
		return "", err
	}
	// The downloadable zip must be portable, so rewrite the absolute path that
	// buildExportDir wrote (for in-app training) to a relative one — it resolves
	// correctly when `yolo train` is run from inside the extracted folder.
	_ = os.WriteFile(filepath.Join(exportDir, "data.yaml"), []byte(buildDataYAML(names, ".")), 0o644)
	zipPath := filepath.Join(s.datasetDir(datasetId), fmt.Sprintf("dataset-%d.zip", datasetId))
	if err := zipDir(exportDir, zipPath); err != nil {
		return "", err
	}
	return zipPath, nil
}

// trainingClassNames returns the model class list for a dataset export: declared
// classes that were actually labeled (declared order preserved for stable class
// indices), then any labeled class not declared (sorted for determinism).
// Declared-but-never-labeled classes are excluded so the trained model has no dead
// output classes (e.g. a "family" class no box ever uses).
func trainingClassNames(declared []string, used map[string]bool) []string {
	names := make([]string, 0, len(used))
	seen := map[string]bool{}
	for _, n := range declared {
		if used[n] && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	extras := make([]string, 0)
	for n := range used {
		if !seen[n] {
			extras = append(extras, n)
		}
	}
	sort.Strings(extras)
	return append(names, extras...)
}

// buildExportDir writes the dataset as a YOLO-format directory (images/{train,val},
// labels/{train,val} with `classIdx cx cy w h`, deterministic 80/20 split, and a
// data.yaml). Only labeled images are exported. Returns the dir and the ordered
// class names used for the class index.
func (s *trainingService) buildExportDir(ctx context.Context, datasetId int64) (string, []string, error) {
	dataset, err := s.datasets.GetById(ctx, "", uint64(datasetId))
	if err != nil {
		return "", nil, err
	}
	images, err := s.listImages(ctx, datasetId)
	if err != nil {
		return "", nil, err
	}

	// Only classes that are actually used in annotations become model classes.
	// declared keeps the dataset's class order so indices stay deterministic, but a
	// declared-but-never-labeled class (e.g. a "Family" dataset listing a "family"
	// class that no box ever uses) is dropped — otherwise the model gets a dead
	// output class that clutters the label picker and confuses users.
	declared := decodeStringList(dataset.Classes)

	exportDir := filepath.Join(s.datasetDir(datasetId), "export")
	_ = os.RemoveAll(exportDir)
	for _, sub := range []string{"images/train", "images/val", "labels/train", "labels/val"} {
		if err := os.MkdirAll(filepath.Join(exportDir, sub), 0o755); err != nil {
			return "", nil, err
		}
	}

	// First pass: load each labeled image with its annotations, recording which
	// classes are actually used. The train/val split is by position (stable) and
	// guarantees both sets are non-empty even for tiny datasets.
	type labeledImage struct {
		base string
		data []byte
		anns []TrainingAnnotation
	}
	labeled := make([]labeledImage, 0, len(images))
	used := map[string]bool{}
	for _, img := range images {
		if img.Status != ImageStatusLabeled && img.Status != ImageStatusReviewed {
			continue
		}
		anns := parseAnnotationsJSON(img.Annotations)
		if len(anns) == 0 {
			continue
		}
		data, err := os.ReadFile(img.FilePath)
		if err != nil {
			continue
		}
		for _, a := range anns {
			used[a.ClassName] = true
		}
		labeled = append(labeled, labeledImage{base: fmt.Sprintf("%d", img.Id), data: data, anns: anns})
	}
	if len(labeled) == 0 {
		return "", nil, errors.New("no labeled images to export — label some images first")
	}

	// Class list = used declared classes (declared order kept for stable indices),
	// then any used class not declared. Unused declared classes are excluded so the
	// trained model has no dead output classes.
	names := trainingClassNames(declared, used)
	index := make(map[string]int, len(names))
	for i, n := range names {
		index[n] = i
	}

	// Second pass: write images + YOLO label files using the final class indices.
	write := func(split string, item labeledImage) error {
		var label strings.Builder
		for _, a := range item.anns {
			idx, ok := index[a.ClassName]
			if !ok {
				continue
			}
			cx := a.X + a.W/2
			cy := a.Y + a.H/2
			fmt.Fprintf(&label, "%d %.6f %.6f %.6f %.6f\n", idx, cx, cy, a.W, a.H)
		}
		if err := os.WriteFile(filepath.Join(exportDir, "images", split, item.base+".jpg"), item.data, 0o644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(exportDir, "labels", split, item.base+".txt"), []byte(label.String()), 0o644)
	}
	for i, item := range labeled {
		// Every 5th image (incl. the first) is validation, the rest are training —
		// guarantees val has at least one. With a single image, use it for both.
		if len(labeled) == 1 {
			if err := write("train", item); err != nil {
				return "", nil, err
			}
			if err := write("val", item); err != nil {
				return "", nil, err
			}
			continue
		}
		split := "train"
		if i%5 == 0 {
			split = "val"
		}
		if err := write(split, item); err != nil {
			return "", nil, err
		}
	}

	// ultralytics resolves a relative data.yaml `path` against its datasets dir /
	// cwd, NOT the yaml's folder — so use an absolute path to the export dir.
	absExport, err := filepath.Abs(exportDir)
	if err != nil {
		absExport = exportDir
	}
	// Forward slashes are valid on Windows for ultralytics/Path and avoid YAML
	// backslash-escaping ambiguity.
	if err := os.WriteFile(filepath.Join(exportDir, "data.yaml"), []byte(buildDataYAML(names, filepath.ToSlash(absExport))), 0o644); err != nil {
		return "", nil, err
	}
	return exportDir, names, nil
}

// buildDataYAML renders an ultralytics data.yaml for the export. Paths are
// relative to the yaml's own directory so the zip is portable.
func buildDataYAML(names []string, basePath string) string {
	if strings.TrimSpace(basePath) == "" {
		basePath = "."
	}
	var b strings.Builder
	// Quote the path so backslashes / spaces in Windows paths are safe in YAML.
	fmt.Fprintf(&b, "path: %q\n", basePath)
	b.WriteString("train: images/train\n")
	b.WriteString("val: images/val\n")
	fmt.Fprintf(&b, "nc: %d\n", len(names))
	b.WriteString("names:\n")
	for _, n := range names {
		fmt.Fprintf(&b, "  - %s\n", n)
	}
	return b.String()
}

func (s *trainingService) modelsDir() string {
	return filepath.Join(s.dataDir, "models")
}

func (s *trainingService) ListModels(ctx context.Context) ([]*entities.TrainingModel, error) {
	rows, _, err := s.models.Get(ctx, modelTable, 1000, 0, nil, []sqldataenums.Sorter{{FieldName: "UpdatedAt", Sort: sqldataenums.DESC}})
	return rows, err
}

func (s *trainingService) GetModel(ctx context.Context, id uint64) (*entities.TrainingModel, error) {
	return s.models.GetById(ctx, modelTable, id)
}

// ImportModel stores externally-trained weights (best.pt) as a registry model.
func (s *trainingService) ImportModel(ctx context.Context, req ImportModelRequest, weights []byte, userId int64) (*entities.TrainingModel, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("model name is required")
	}
	if len(weights) == 0 {
		return nil, errors.New("model weights file is required")
	}
	classesJSON := mustJSON(normalizeMembers(req.Classes, false))
	now := s.now().UTC().Unix()
	row := entities.TrainingModel{
		Name:      name,
		BaseModel: strings.TrimSpace(req.BaseModel),
		Status:    "imported",
		Classes:   string(classesJSON),
		CreatedBy: userId,
		CreatedAt: now,
		UpdatedBy: userId,
		UpdatedAt: now,
	}
	id, err := s.models.Create(ctx, modelTable, row)
	if err != nil {
		return nil, err
	}
	row.Id = int64(id)

	dir := filepath.Join(s.modelsDir(), fmt.Sprintf("%d", row.Id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		_, _ = s.models.DeleteById(ctx, modelTable, id)
		return nil, err
	}
	path := filepath.Join(dir, "best.pt")
	if err := os.WriteFile(path, weights, 0o644); err != nil {
		_, _ = s.models.DeleteById(ctx, modelTable, id)
		return nil, err
	}
	row.FilePath = path
	if _, err := s.models.UpdateById(ctx, modelTable, row); err != nil {
		return nil, err
	}
	return &row, nil
}

// ActivateModel points the worker at this model's weights (hot-swapping it),
// marks it active, and registers its classes so detection rules can target them.
func (s *trainingService) ActivateModel(ctx context.Context, id uint64, userId int64) (*entities.TrainingModel, error) {
	model, err := s.models.GetById(ctx, modelTable, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(model.FilePath) == "" {
		return nil, errors.New("model has no weights file")
	}
	if _, err := os.Stat(model.FilePath); err != nil {
		return nil, fmt.Errorf("model weights not found: %w", err)
	}

	// Write the active-model pointer the YOLO worker reads on (re)start.
	if s.activeModelFile != "" {
		if err := os.MkdirAll(filepath.Dir(s.activeModelFile), 0o755); err != nil {
			return nil, err
		}
		abs, _ := filepath.Abs(model.FilePath)
		if err := os.WriteFile(s.activeModelFile, []byte(abs), 0o644); err != nil {
			return nil, err
		}
	}

	// Flip the active flag: clear others, set this one.
	all, err := s.ListModels(ctx)
	if err == nil {
		now := s.now().UTC().Unix()
		for _, m := range all {
			active := m.Id == model.Id
			if m.IsActive == active {
				continue
			}
			m.IsActive = active
			m.UpdatedAt = now
			_, _ = s.models.UpdateById(ctx, modelTable, *m)
		}
	}
	model.IsActive = true

	// Hot-swap: stop the worker so the next inference reloads with the new model.
	if reloader, ok := s.detector.(interface{ Reload() error }); ok {
		_ = reloader.Reload()
	}

	// Register the model's classes so rules can target them immediately.
	if s.classes != nil {
		_ = s.classes.UpsertTrained(ctx, decodeStringList(model.Classes), userId)
	}
	return model, nil
}

// DeactivateModel reverts detection to the stock model: it clears the active-model
// pointer, marks all models inactive, and reloads the worker (which then falls
// back to the bundled yolo11n.pt). After this, an inactive model can be deleted.
func (s *trainingService) DeactivateModel(ctx context.Context, userId int64) error {
	if s.activeModelFile != "" {
		_ = os.Remove(s.activeModelFile)
	}
	all, err := s.ListModels(ctx)
	if err == nil {
		now := s.now().UTC().Unix()
		for _, m := range all {
			if !m.IsActive {
				continue
			}
			m.IsActive = false
			m.UpdatedBy = userId
			m.UpdatedAt = now
			_, _ = s.models.UpdateById(ctx, modelTable, *m)
		}
	}
	if reloader, ok := s.detector.(interface{ Reload() error }); ok {
		_ = reloader.Reload()
	}
	return nil
}

func (s *trainingService) DeleteModel(ctx context.Context, id uint64) (uint64, error) {
	model, err := s.models.GetById(ctx, modelTable, id)
	if err != nil {
		return 0, err
	}
	if model.IsActive {
		return 0, errors.New("cannot delete the active model — activate another first")
	}
	_ = os.RemoveAll(filepath.Join(s.modelsDir(), fmt.Sprintf("%d", model.Id)))
	return s.models.DeleteById(ctx, modelTable, id)
}

func decodeStringList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil
	}
	return normalizeMembers(out, false)
}

// parseAnnotationsJSON decodes the stored annotation array (top-left x/y + w/h).
func parseAnnotationsJSON(value string) []TrainingAnnotation {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var anns []TrainingAnnotation
	if err := json.Unmarshal([]byte(value), &anns); err != nil {
		return nil
	}
	out := make([]TrainingAnnotation, 0, len(anns))
	for _, a := range anns {
		a.ClassName = strings.ToLower(strings.TrimSpace(a.ClassName))
		if a.ClassName == "" || a.W <= 0 || a.H <= 0 {
			continue
		}
		out = append(out, a)
	}
	return out
}

// zipDir writes a zip of srcDir (relative paths) to zipPath.
func zipDir(srcDir, zipPath string) error {
	out, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	defer zw.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
}
