package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for DecodeConfig
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/atrest"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/vision"
)

// Image source + status values.
const (
	ImageSourceUpload = "upload"
	ImageSourceAlert  = "alert"

	ImageStatusUnlabeled = "unlabeled"
	ImageStatusLabeled   = "labeled"
	ImageStatusReviewed  = "reviewed"

	maxTrainingImageBytes = 12 * 1024 * 1024
)

// TrainingDatasetRequest is the create/update shape for a dataset.
type TrainingDatasetRequest struct {
	Id          int64    `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Classes     []string `json:"classes"`
}

// TrainingAnnotation is one labeled box on an image, in normalized 0..1 coords.
type TrainingAnnotation struct {
	ClassName string  `json:"className"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	W         float64 `json:"w"`
	H         float64 `json:"h"`
	Source    string  `json:"source"`
}

type trainingService struct {
	datasets        dbsql.IGenericRepo[entities.TrainingDataset]
	images          dbsql.IGenericRepo[entities.TrainingImage]
	models          dbsql.IGenericRepo[entities.TrainingModel]
	vision          IVisionService
	classes         IDetectionClassService
	dataDir         string
	activeModelFile string
	stockModelFile  string
	// lprModelFile is the pointer file the YOLO worker reads to load the optional
	// license-plate detector (separate slot from stock/custom — it's a second-stage
	// detector, not merged into general detection).
	lprModelFile string
	// detector is the shared object-detection backend (also used by the live
	// monitor). nil in motion mode — auto-label then reports it is unavailable.
	detector      vision.ObjectDetector
	minConfidence float64
	trainCfg      TrainingRunConfig
	// cipher (optional) encrypts dataset images at rest. nil = plaintext.
	cipher *atrest.Cipher
	now    func() time.Time

	trainMu  sync.Mutex
	training bool

	// in-app dependency installer state (one run at a time, polled by the UI)
	setupMu      sync.Mutex
	setupRunning bool
	setupStatus  string
	setupLog     string
}

// NewTrainingService creates the custom-model training service. detector is the
// shared object backend (may be nil); classes is the registry that trained model
// classes are upserted into on activation; activeModelFile is the pointer file
// the YOLO worker reads to hot-swap models.
func NewTrainingService(
	datasets dbsql.IGenericRepo[entities.TrainingDataset],
	images dbsql.IGenericRepo[entities.TrainingImage],
	models dbsql.IGenericRepo[entities.TrainingModel],
	vision IVisionService,
	classes IDetectionClassService,
	dataDir string,
	activeModelFile string,
	stockModelFile string,
	lprModelFile string,
	detector vision.ObjectDetector,
	minConfidence float64,
	trainCfg TrainingRunConfig,
	cipher *atrest.Cipher,
) ITrainingService {
	dir := strings.TrimSpace(dataDir)
	if dir == "" {
		dir = "training"
	}
	if minConfidence <= 0 {
		minConfidence = 0.25
	}
	return &trainingService{
		datasets:        datasets,
		images:          images,
		models:          models,
		vision:          vision,
		classes:         classes,
		dataDir:         dir,
		activeModelFile: strings.TrimSpace(activeModelFile),
		stockModelFile:  strings.TrimSpace(stockModelFile),
		lprModelFile:    strings.TrimSpace(lprModelFile),
		detector:        detector,
		minConfidence:   minConfidence,
		trainCfg:        trainCfg,
		cipher:          cipher,
		now:             time.Now,
	}
}

// encryptImage / decryptImage apply encryption-at-rest to dataset image bytes when a
// cipher is configured (nil = plaintext). decryptImage passes legacy plaintext through.
func (s *trainingService) encryptImage(data []byte) []byte {
	if s.cipher == nil {
		return data
	}
	if enc, err := s.cipher.EncryptBytes(data); err == nil {
		return enc
	}
	return data
}

func (s *trainingService) decryptImage(data []byte) []byte {
	if s.cipher == nil {
		return data
	}
	if dec, err := s.cipher.DecryptBytes(data); err == nil {
		return dec
	}
	return data
}

// GetImageBytes reads a dataset image and decrypts it (legacy plaintext passes
// through), so callers/serving never see ciphertext.
func (s *trainingService) GetImageBytes(ctx context.Context, id uint64) ([]byte, error) {
	img, err := s.images.GetById(ctx, "", id)
	if err != nil {
		return nil, err
	}
	if img == nil || strings.TrimSpace(img.FilePath) == "" {
		return nil, errors.New("image file is missing")
	}
	data, err := os.ReadFile(img.FilePath)
	if err != nil {
		return nil, err
	}
	return s.decryptImage(data), nil
}

func (s *trainingService) objectDetector() (vision.ObjectDetector, error) {
	if s.detector == nil {
		return nil, errors.New("auto-label is unavailable: the detector is not in external/persistent mode")
	}
	return s.detector, nil
}

func (s *trainingService) ListDatasets(ctx context.Context) ([]*entities.TrainingDataset, error) {
	rows, _, err := s.datasets.Get(ctx, "", 1000, 0, nil, []sqldataenums.Sorter{{FieldName: "UpdatedAt", Sort: sqldataenums.DESC}})
	return rows, err
}

func (s *trainingService) GetDataset(ctx context.Context, id uint64) (*entities.TrainingDataset, error) {
	return s.datasets.GetById(ctx, "", id)
}

func (s *trainingService) SaveDataset(ctx context.Context, req TrainingDatasetRequest, userId int64) (*entities.TrainingDataset, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("dataset name is required")
	}
	classesJSON, _ := json.Marshal(normalizeMembers(req.Classes, false))
	now := s.now().UTC().Unix()
	row := entities.TrainingDataset{
		Id:          req.Id,
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		Classes:     string(classesJSON),
		Status:      "draft",
		UpdatedBy:   userId,
		UpdatedAt:   now,
	}
	if row.Id > 0 {
		existing, err := s.datasets.GetById(ctx, "", uint64(row.Id))
		if err != nil {
			return nil, err
		}
		row.CreatedAt = existing.CreatedAt
		row.CreatedBy = existing.CreatedBy
		row.ImageCount = existing.ImageCount
		row.LabeledCount = existing.LabeledCount
		row.Status = existing.Status
		if _, err := s.datasets.UpdateById(ctx, "", row); err != nil {
			return nil, err
		}
		return &row, nil
	}
	row.CreatedBy = userId
	row.CreatedAt = now
	id, err := s.datasets.Create(ctx, "", row)
	if err != nil {
		return nil, err
	}
	row.Id = int64(id)
	return &row, nil
}

func (s *trainingService) DeleteDataset(ctx context.Context, id uint64) (uint64, error) {
	images, err := s.listImages(ctx, int64(id))
	if err != nil {
		return 0, err
	}
	for _, img := range images {
		if img.FilePath != "" {
			_ = os.Remove(img.FilePath)
		}
		_, _ = s.images.DeleteById(ctx, "", uint64(img.Id))
	}
	_ = os.RemoveAll(s.datasetDir(int64(id)))
	return s.datasets.DeleteById(ctx, "", id)
}

func (s *trainingService) ListImages(ctx context.Context, datasetId int64) ([]*entities.TrainingImage, error) {
	return s.listImages(ctx, datasetId)
}

func (s *trainingService) GetImage(ctx context.Context, id uint64) (*entities.TrainingImage, error) {
	return s.images.GetById(ctx, "", id)
}

// StoreUpload writes an uploaded JPEG into the dataset and records its row.
func (s *trainingService) StoreUpload(ctx context.Context, datasetId int64, data []byte, userId int64) (*entities.TrainingImage, error) {
	if _, err := s.datasets.GetById(ctx, "", uint64(datasetId)); err != nil {
		return nil, fmt.Errorf("dataset not found: %w", err)
	}
	return s.storeImage(ctx, datasetId, data, ImageSourceUpload, nil, userId)
}

// AddFromAlert copies an alert's snapshot into the dataset and seeds an
// annotation from the alert's bounding box so it lands pre-labeled.
func (s *trainingService) AddFromAlert(ctx context.Context, datasetId int64, alertId int64, userId int64) (*entities.TrainingImage, error) {
	if _, err := s.datasets.GetById(ctx, "", uint64(datasetId)); err != nil {
		return nil, fmt.Errorf("dataset not found: %w", err)
	}
	alert, err := s.vision.GetAlertById(ctx, uint64(alertId))
	if err != nil {
		return nil, err
	}
	if alert == nil || strings.TrimSpace(alert.SnapshotPath) == "" {
		return nil, errors.New("alert has no snapshot to import")
	}
	data, err := os.ReadFile(alert.SnapshotPath)
	if err != nil {
		return nil, fmt.Errorf("read alert snapshot: %w", err)
	}
	data = s.decryptImage(data) // alert snapshots are encrypted at rest
	var seed []TrainingAnnotation
	if ann := annotationFromAlert(alert); ann != nil {
		seed = []TrainingAnnotation{*ann}
	}
	return s.storeImage(ctx, datasetId, data, ImageSourceAlert, seed, userId)
}

func (s *trainingService) DeleteImage(ctx context.Context, id uint64) (uint64, error) {
	img, err := s.images.GetById(ctx, "", id)
	if err != nil {
		return 0, err
	}
	if img.FilePath != "" {
		_ = os.Remove(img.FilePath)
	}
	count, err := s.images.DeleteById(ctx, "", id)
	if err != nil {
		return 0, err
	}
	_ = s.refreshDatasetCounts(ctx, img.DatasetId)
	return count, nil
}

// SaveAnnotations replaces an image's annotations (normalized, validated) and
// updates its labeled status + the dataset counts.
func (s *trainingService) SaveAnnotations(ctx context.Context, imageId int64, annotations []TrainingAnnotation, userId int64) (*entities.TrainingImage, error) {
	img, err := s.images.GetById(ctx, "", uint64(imageId))
	if err != nil {
		return nil, err
	}
	clean := normalizeAnnotations(annotations, "manual")
	encoded, _ := json.Marshal(clean)
	img.Annotations = string(encoded)
	if len(clean) > 0 {
		img.Status = ImageStatusLabeled
	} else {
		img.Status = ImageStatusUnlabeled
	}
	img.UpdatedBy = userId
	img.UpdatedAt = s.now().UTC().Unix()
	if _, err := s.images.UpdateById(ctx, "", *img); err != nil {
		return nil, err
	}
	used := make([]string, 0, len(clean))
	for _, a := range clean {
		used = append(used, a.ClassName)
	}
	_ = s.refreshDataset(ctx, img.DatasetId, used)
	return img, nil
}

// AutoLabel runs the object detector on an image and stores the resulting boxes
// as "auto" annotations the user can then correct.
func (s *trainingService) AutoLabel(ctx context.Context, imageId int64, userId int64) (*entities.TrainingImage, error) {
	img, err := s.images.GetById(ctx, "", uint64(imageId))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(img.FilePath) == "" {
		return nil, errors.New("image file is missing")
	}
	data, err := os.ReadFile(img.FilePath)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	data = s.decryptImage(data)
	detector, err := s.objectDetector()
	if err != nil {
		return nil, err
	}
	candidates, err := detector.DetectObjects(ctx, vision.Frame{
		Data:       data,
		Format:     "jpeg",
		Width:      img.Width,
		Height:     img.Height,
		CapturedAt: s.now().UTC().Unix(),
	})
	if err != nil {
		return nil, fmt.Errorf("auto-label inference failed: %w", err)
	}
	annotations := make([]TrainingAnnotation, 0, len(candidates))
	for _, c := range candidates {
		if c.Confidence < s.minConfidence {
			continue
		}
		label := strings.ToLower(strings.TrimSpace(c.Label))
		if label == "" {
			continue
		}
		annotations = append(annotations, TrainingAnnotation{
			ClassName: label,
			X:         c.Box.X,
			Y:         c.Box.Y,
			W:         c.Box.W,
			H:         c.Box.H,
			Source:    "auto",
		})
	}
	clean := normalizeAnnotations(annotations, "auto")
	encoded, _ := json.Marshal(clean)
	img.Annotations = string(encoded)
	if len(clean) > 0 {
		img.Status = ImageStatusLabeled
	}
	img.UpdatedBy = userId
	img.UpdatedAt = s.now().UTC().Unix()
	if _, err := s.images.UpdateById(ctx, "", *img); err != nil {
		return nil, err
	}
	_ = s.refreshDatasetCounts(ctx, img.DatasetId)
	return img, nil
}

// normalizeAnnotations clamps boxes to 0..1, drops empty/degenerate ones, and
// lowercases class names. Empty source defaults to defaultSource.
func normalizeAnnotations(annotations []TrainingAnnotation, defaultSource string) []TrainingAnnotation {
	clean := make([]TrainingAnnotation, 0, len(annotations))
	for _, a := range annotations {
		className := strings.ToLower(strings.TrimSpace(a.ClassName))
		x := clamp01(a.X)
		y := clamp01(a.Y)
		w := clamp01(a.W)
		h := clamp01(a.H)
		if x+w > 1 {
			w = 1 - x
		}
		if y+h > 1 {
			h = 1 - y
		}
		if className == "" || w <= 0.001 || h <= 0.001 {
			continue
		}
		source := strings.ToLower(strings.TrimSpace(a.Source))
		if source == "" {
			source = defaultSource
		}
		clean = append(clean, TrainingAnnotation{ClassName: className, X: x, Y: y, W: w, H: h, Source: source})
	}
	return clean
}

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

// storeImage decodes dimensions, persists the row to mint an id, writes the file
// under the dataset's images dir, then records the path.
func (s *trainingService) storeImage(ctx context.Context, datasetId int64, data []byte, source string, seed []TrainingAnnotation, userId int64) (*entities.TrainingImage, error) {
	if len(data) == 0 {
		return nil, errors.New("image is empty")
	}
	if len(data) > maxTrainingImageBytes {
		return nil, errors.New("image exceeds size limit")
	}
	width, height := 0, 0
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		width, height = cfg.Width, cfg.Height
	} else {
		return nil, fmt.Errorf("unsupported image (JPEG required): %w", err)
	}

	status := ImageStatusUnlabeled
	annotationsJSON := "[]"
	if len(seed) > 0 {
		status = ImageStatusLabeled
		if encoded, err := json.Marshal(seed); err == nil {
			annotationsJSON = string(encoded)
		}
	}

	now := s.now().UTC().Unix()
	row := entities.TrainingImage{
		DatasetId:   datasetId,
		Width:       width,
		Height:      height,
		Source:      source,
		Status:      status,
		Annotations: annotationsJSON,
		CreatedBy:   userId,
		CreatedAt:   now,
		UpdatedBy:   userId,
		UpdatedAt:   now,
	}
	id, err := s.images.Create(ctx, "", row)
	if err != nil {
		return nil, err
	}
	row.Id = int64(id)

	dir := filepath.Join(s.datasetDir(datasetId), "images")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		_, _ = s.images.DeleteById(ctx, "", id)
		return nil, err
	}
	path := filepath.Join(dir, fmt.Sprintf("%d.jpg", row.Id))
	if err := os.WriteFile(path, s.encryptImage(data), 0o644); err != nil {
		_, _ = s.images.DeleteById(ctx, "", id)
		return nil, err
	}
	row.FilePath = path
	if _, err := s.images.UpdateById(ctx, "", row); err != nil {
		return nil, err
	}
	_ = s.refreshDatasetCounts(ctx, datasetId)
	return &row, nil
}

func (s *trainingService) listImages(ctx context.Context, datasetId int64) ([]*entities.TrainingImage, error) {
	filters := []sqldataenums.Filter{{FieldName: "DatasetId", Compare: sqldataenums.Equal, Value: datasetId}}
	sorters := []sqldataenums.Sorter{{FieldName: "CreatedAt", Sort: sqldataenums.DESC}}
	rows, _, err := s.images.Get(ctx, "", 5000, 0, filters, sorters)
	return rows, err
}

func (s *trainingService) refreshDatasetCounts(ctx context.Context, datasetId int64) error {
	return s.refreshDataset(ctx, datasetId, nil)
}

// refreshDataset recomputes the dataset's image/labeled counts and unions any
// addClasses into its class list (so labels the user assigns while annotating
// persist on the dataset and pre-populate future images), in one write.
func (s *trainingService) refreshDataset(ctx context.Context, datasetId int64, addClasses []string) error {
	images, err := s.listImages(ctx, datasetId)
	if err != nil {
		return err
	}
	labeled := 0
	for _, img := range images {
		if img.Status == ImageStatusLabeled || img.Status == ImageStatusReviewed {
			labeled++
		}
	}
	dataset, err := s.datasets.GetById(ctx, "", uint64(datasetId))
	if err != nil {
		return err
	}
	dataset.ImageCount = len(images)
	dataset.LabeledCount = labeled
	if len(addClasses) > 0 {
		dataset.Classes = string(mustJSON(unionClasses(dataset.Classes, addClasses)))
	}
	dataset.UpdatedAt = s.now().UTC().Unix()
	_, err = s.datasets.UpdateById(ctx, "", *dataset)
	return err
}

// unionClasses merges new class slugs into an existing JSON class array,
// preserving order and de-duplicating.
func unionClasses(existingJSON string, add []string) []string {
	var existing []string
	if strings.TrimSpace(existingJSON) != "" {
		_ = json.Unmarshal([]byte(existingJSON), &existing)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(existing)+len(add))
	for _, c := range append(existing, add...) {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func (s *trainingService) datasetDir(datasetId int64) string {
	return filepath.Join(s.dataDir, "datasets", fmt.Sprintf("%d", datasetId))
}

// annotationFromAlert builds a seed annotation from an alert's bounding box,
// preferring the detected object label from metadata over the rule type.
func annotationFromAlert(alert *entities.AlertEvent) *TrainingAnnotation {
	box := strings.TrimSpace(alert.BoundingBox)
	if box == "" {
		return nil
	}
	var b struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W float64 `json:"w"`
		H float64 `json:"h"`
	}
	if err := json.Unmarshal([]byte(box), &b); err != nil || b.W <= 0 || b.H <= 0 {
		return nil
	}
	className := strings.ToLower(strings.TrimSpace(alert.DetectionType))
	var meta struct {
		ObjectLabel string `json:"objectLabel"`
	}
	if json.Unmarshal([]byte(alert.Metadata), &meta) == nil && strings.TrimSpace(meta.ObjectLabel) != "" {
		className = strings.ToLower(strings.TrimSpace(meta.ObjectLabel))
	}
	return &TrainingAnnotation{ClassName: className, X: b.X, Y: b.Y, W: b.W, H: b.H, Source: "auto"}
}
