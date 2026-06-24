package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// LPRModelOption is one entry in the curated plate-detector catalog the app can
// download on demand (the "automated" path), mirroring the stock-model picker.
// Unlike stock models (official ultralytics assets fetched by name), plate models
// are third-party, so each carries an explicit download URL.
type LPRModelOption struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

// LPRModelInfo is the current plate model plus the catalog the UI offers.
type LPRModelInfo struct {
	// Current is the active plate model's display name, or "" when LPR is disabled.
	Current string `json:"current"`
	// Path is the resolved local weights path (empty when disabled).
	Path string `json:"path"`
	// Options are the curated, downloadable plate detectors.
	Options []LPRModelOption `json:"options"`
	// OcrReady reports whether the OCR dependency (easyocr) is importable in the
	// app's Python. False = a model can be set but plate text won't be read until
	// the OCR deps are installed.
	OcrReady bool `json:"ocrReady"`
}

// lprModelCatalog is a small curated list of community plate detectors. URLs are
// direct .pt downloads (verified public, no auth). Maintainers should re-check
// these periodically; the user can also paste an arbitrary https URL or upload a
// .pt manually, so the feature does not depend on any single entry staying live.
// Models are YOLOv11 plate finetunes (ultralytics, AGPL) hosted on Hugging Face.
var lprModelCatalog = []LPRModelOption{
	{
		Name:        "yolo11n-license-plate",
		Description: "YOLOv11 nano plate detector — fastest, good for CPU / low load.",
		URL:         "https://huggingface.co/morsetechlab/yolov11-license-plate-detection/resolve/main/license-plate-finetune-v1n.pt",
	},
	{
		Name:        "yolo11s-license-plate",
		Description: "YOLOv11 small plate detector — more accurate; GPU recommended.",
		URL:         "https://huggingface.co/morsetechlab/yolov11-license-plate-detection/resolve/main/license-plate-finetune-v1s.pt",
	},
}

// GetLPRModel returns the active plate model (from the pointer file) plus the
// downloadable catalog.
func (s *trainingService) GetLPRModel(ctx context.Context) LPRModelInfo {
	info := LPRModelInfo{Options: lprModelCatalog}
	if s.lprModelFile != "" {
		if data, err := os.ReadFile(s.lprModelFile); err == nil {
			if p := strings.TrimSpace(string(data)); p != "" && fileExists(p) {
				info.Path = p
				info.Current = filepath.Base(p)
			}
		}
	}
	info.OcrReady = s.lprOcrReady(ctx)
	return info
}

// lprOcrReady reports whether easyocr can be imported in the app's Python. A quick
// best-effort probe (short timeout); any failure is treated as "not ready".
func (s *trainingService) lprOcrReady(ctx context.Context) bool {
	py := strings.TrimSpace(s.trainCfg.PythonCmd)
	if py == "" {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	// importlib.util.find_spec avoids the heavy easyocr import (which loads torch);
	// it only checks the package is installed.
	out, err := exec.CommandContext(probeCtx, py, "-c", "import importlib.util,sys; sys.stdout.write('1' if importlib.util.find_spec('easyocr') else '0')").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "1"
}

// SetLPRModel selects the active plate model and reloads the worker. The value is
// one of: a catalog name (downloaded), an https URL (downloaded), a local .pt
// path, or "" / "none" to disable LPR.
func (s *trainingService) SetLPRModel(ctx context.Context, value string, userId int64) error {
	if s.lprModelFile == "" {
		return errors.New("license-plate model is not configurable on this host")
	}
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "none") || strings.EqualFold(value, "default") {
		_ = os.Remove(s.lprModelFile)
		s.reloadDetector()
		return nil
	}

	resolved := ""
	switch {
	case isHTTPURL(value):
		path, err := s.downloadPlateModel(value, plateModelFileName(value))
		if err != nil {
			return err
		}
		resolved = path
	default:
		// Catalog name match → download its URL; else treat as a local path.
		if opt, ok := lprCatalogByName(value); ok {
			path, err := s.downloadPlateModel(opt.URL, opt.Name+".pt")
			if err != nil {
				return err
			}
			resolved = path
		} else {
			if _, err := os.Stat(value); err != nil {
				return fmt.Errorf("model file not found: %s", value)
			}
			resolved, _ = filepath.Abs(value)
		}
	}

	if err := s.writeLPRPointer(resolved); err != nil {
		return err
	}
	s.reloadDetector()
	return nil
}

// ImportLPRModel stores an uploaded plate-detector .pt and activates it (writes the
// pointer + reloads the worker). Mirrors ImportModel but targets the LPR slot.
func (s *trainingService) ImportLPRModel(ctx context.Context, name string, weights []byte, userId int64) (LPRModelInfo, error) {
	if s.lprModelFile == "" {
		return LPRModelInfo{}, errors.New("license-plate model is not configurable on this host")
	}
	if len(weights) == 0 {
		return LPRModelInfo{}, errors.New("model weights file is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "plate-model"
	}
	dir := filepath.Join(s.modelsDir(), "lpr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return LPRModelInfo{}, err
	}
	path := filepath.Join(dir, sanitizeModelFileName(name))
	if err := os.WriteFile(path, weights, 0o644); err != nil {
		return LPRModelInfo{}, err
	}
	abs, _ := filepath.Abs(path)
	if err := s.writeLPRPointer(abs); err != nil {
		return LPRModelInfo{}, err
	}
	s.reloadDetector()
	return s.GetLPRModel(ctx), nil
}

// DeactivateLPRModel clears the plate-model pointer (disabling LPR) and reloads
// the worker so the OCR stage stops running.
func (s *trainingService) DeactivateLPRModel(ctx context.Context, userId int64) error {
	if s.lprModelFile == "" {
		return nil
	}
	_ = os.Remove(s.lprModelFile)
	s.reloadDetector()
	return nil
}

// lprRequirements returns the absolute path to requirements-lpr.txt (next to the
// train worker / setup scripts), or "" if it can't be located.
func (s *trainingService) lprRequirements() string {
	trainScript := strings.TrimSpace(s.trainCfg.TrainScript)
	if trainScript == "" {
		return ""
	}
	req := filepath.Join(filepath.Dir(trainScript), "requirements-lpr.txt")
	if !fileExists(req) {
		return ""
	}
	abs, err := filepath.Abs(req)
	if err != nil {
		return req
	}
	return abs
}

// StartLPRDepsSetup installs the optional license-plate OCR dependencies (easyocr +
// opencv + numpy) into the app's Python, streaming progress to the SAME shared
// installer log the GPU setup uses (one install at a time, polled via
// DepsSetupStatus). Unlike the GPU install it does NOT require a CUDA GPU — easyocr
// runs on CPU — and it does a targeted pip install rather than a full torch reinstall.
func (s *trainingService) StartLPRDepsSetup(ctx context.Context) error {
	py := strings.TrimSpace(s.trainCfg.PythonCmd)
	if py == "" {
		return errors.New("python is not configured on this host")
	}
	req := s.lprRequirements()

	s.setupMu.Lock()
	if s.setupRunning {
		s.setupMu.Unlock()
		return errors.New("a dependency install is already in progress")
	}
	s.setupRunning = true
	s.setupStatus = "running"
	s.setupLog = "Installing license-plate OCR dependencies (easyocr) — this can take a few minutes...\n"
	s.setupMu.Unlock()

	go s.runLPRDepsSetup(py, req)
	return nil
}

// runLPRDepsSetup pip-installs the OCR deps, pausing the detector around the
// install so any shared files (numpy/opencv) aren't locked on Windows.
func (s *trainingService) runLPRDepsSetup(py string, requirements string) {
	defer func() {
		s.setupMu.Lock()
		s.setupRunning = false
		s.setupMu.Unlock()
	}()

	if p, ok := s.detector.(interface {
		Pause()
		Resume()
	}); ok {
		p.Pause()
		defer p.Resume()
	}

	// Prefer the requirements file (pins the set); fall back to explicit packages
	// when it can't be located (e.g. a trimmed deploy).
	args := []string{"-m", "pip", "install", "--upgrade"}
	if requirements != "" {
		args = append(args, "-r", requirements)
	} else {
		args = append(args, "easyocr", "opencv-python", "numpy")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, py, args...)
	writer := &setupLogWriter{s: s}
	cmd.Stdout = writer
	cmd.Stderr = writer

	err := cmd.Run()
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if err != nil {
		s.setupStatus = "failed"
		s.setupLog += "\n[failed] " + err.Error() + "\n"
		return
	}
	s.setupStatus = "done"
	s.setupLog += "\n[done] OCR dependencies installed. Restart the server (or reload the plate model) to enable plate reading.\n"
}

func (s *trainingService) writeLPRPointer(resolved string) error {
	if err := os.MkdirAll(filepath.Dir(s.lprModelFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.lprModelFile, []byte(resolved), 0o644)
}

// downloadPlateModel fetches a .pt from an https URL into the lpr models dir and
// returns its absolute path. Plate models are not ultralytics assets, so this is a
// plain HTTP download (vs. the stock model's ultralytics-by-name fetch).
func (s *trainingService) downloadPlateModel(url string, fileName string) (string, error) {
	if !isHTTPURL(url) {
		return "", fmt.Errorf("invalid model URL: %s", url)
	}
	dir := filepath.Join(s.modelsDir(), "lpr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, sanitizeModelFileName(fileName))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	// Some model hosts (Hugging Face CDN, GitHub) reject the default Go user agent.
	req.Header.Set("User-Agent", "MyMataSan/1.0")
	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download returned %s", resp.Status)
	}

	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	// Cap at 512 MiB — plate detectors are small; this guards against a wrong URL
	// streaming something huge.
	if _, err := io.Copy(out, io.LimitReader(resp.Body, 512*1024*1024)); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return "", fmt.Errorf("download write failed: %w", err)
	}
	_ = out.Close()
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	abs, _ := filepath.Abs(dest)
	return abs, nil
}

func isHTTPURL(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://")
}

func lprCatalogByName(name string) (LPRModelOption, bool) {
	for _, opt := range lprModelCatalog {
		if strings.EqualFold(opt.Name, name) {
			return opt, true
		}
	}
	return LPRModelOption{}, false
}

// plateModelFileName derives a local filename from a download URL, falling back to
// a generic name when the URL has no usable basename.
func plateModelFileName(url string) string {
	base := url
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if q := strings.IndexAny(base, "?#"); q >= 0 {
		base = base[:q]
	}
	base = strings.TrimSpace(base)
	if base == "" || !strings.HasSuffix(strings.ToLower(base), ".pt") {
		return "plate-model.pt"
	}
	return base
}

// sanitizeModelFileName keeps a safe .pt filename (no path separators) for storage.
func sanitizeModelFileName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "_")
	if name == "" || name == "." || name == ".." {
		name = "plate-model.pt"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pt") {
		name += ".pt"
	}
	return name
}
