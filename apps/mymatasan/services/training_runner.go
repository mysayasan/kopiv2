package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/infra/externaltools"
)

// TrainingRunConfig wires the in-app trainer: the Python command, the train
// worker script, and the default base weights to fine-tune from.
type TrainingRunConfig struct {
	PythonCmd   string
	TrainScript string
	BaseModel   string
	// ConfigFile is the absolute path of the app config.json. The dependency
	// installer rewrites vision.detector.command here when it has to switch to a
	// different (GPU-capable) Python than the one currently configured.
	ConfigFile string
}

// MachineCapability reports whether in-app training is possible on this host and
// whether a CUDA GPU is available (CPU training works but is impractically slow).
type MachineCapability struct {
	Python        bool   `json:"python"`
	PythonVersion string `json:"pythonVersion"`
	// PythonGpuCapable reports whether the configured Python is a version PyTorch
	// ships CUDA wheels for (CPython 3.9-3.13). A newer interpreter (e.g. 3.14)
	// can only ever run on CPU regardless of the GPU.
	PythonGpuCapable bool   `json:"pythonGpuCapable"`
	Torch            bool   `json:"torch"`
	TorchVersion     string `json:"torchVersion"`
	Ultralytics      bool   `json:"ultralytics"`
	Cuda             bool   `json:"cuda"`
	Device           string `json:"device"`
	// HasGpu reports a CUDA-capable NVIDIA GPU + driver detected on the host
	// (via nvidia-smi), independent of whether PyTorch can currently use it.
	HasGpu    bool   `json:"hasGpu"`
	Gpu       string `json:"gpu"`
	Available bool   `json:"available"`
	// CanInstall is true when the in-app dependency installer can run here: a
	// CUDA-capable GPU is present and the setup script exists.
	CanInstall bool   `json:"canInstall"`
	Detail     string `json:"detail"`
}

// StartTrainingRequest is the body for kicking off an in-app training run.
type StartTrainingRequest struct {
	DatasetId int64  `json:"datasetId"`
	Name      string `json:"name"`
	Epochs    int    `json:"epochs"`
	Imgsz     int    `json:"imgsz"`
}

// MachineCapability probes Python, ultralytics, and CUDA availability.
func (s *trainingService) MachineCapability(ctx context.Context) MachineCapability {
	cap := MachineCapability{}
	cmd := strings.TrimSpace(s.trainCfg.PythonCmd)
	if cmd == "" {
		cap.Detail = "no Python command configured"
		return cap
	}
	tool := externaltools.CheckExecutable(ctx, externaltools.ExecutableSpec{
		Name:           "python",
		ConfiguredPath: cmd,
		ExecutableName: cmd,
		ProbeArgs:      []string{"--version"},
		Timeout:        5 * time.Second,
		MaxOutputBytes: 2048,
	})
	if !tool.Found {
		cap.Detail = "Python was not found"
		return cap
	}
	cap.Python = true

	// Reports torch presence + whether it's a CUDA build, so we can tell a missing
	// GPU apart from a CPU-only torch build (the common Windows pitfall).
	script := "import json, sys, importlib.util\n" +
		"info = {'py': '%d.%d' % sys.version_info[:2], 'torch': '', 'cudaBuild': '', 'cuda': False, 'device': '', 'ultralytics': importlib.util.find_spec('ultralytics') is not None}\n" +
		"try:\n" +
		" import torch\n" +
		" info['torch'] = torch.__version__\n" +
		" info['cudaBuild'] = torch.version.cuda or ''\n" +
		" info['cuda'] = bool(torch.cuda.is_available())\n" +
		" info['device'] = torch.cuda.get_device_name(0) if torch.cuda.is_available() else ''\n" +
		"except Exception:\n pass\n" +
		"print('CAP:' + json.dumps(info))\n"
	out, err := externaltools.Probe(ctx, tool.Path, []string{"-c", script}, 30*time.Second, 8192)
	cudaBuild := ""
	if err == nil {
		var probe struct {
			Py          string `json:"py"`
			Torch       string `json:"torch"`
			CudaBuild   string `json:"cudaBuild"`
			Cuda        bool   `json:"cuda"`
			Device      string `json:"device"`
			Ultralytics bool   `json:"ultralytics"`
		}
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "CAP:") {
				continue
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "CAP:")), &probe) == nil {
				cap.PythonVersion = probe.Py
				cap.Torch = strings.TrimSpace(probe.Torch) != ""
				cap.TorchVersion = probe.Torch
				cap.Cuda = probe.Cuda
				cap.Device = probe.Device
				cap.Ultralytics = probe.Ultralytics
				cudaBuild = probe.CudaBuild
			}
			break
		}
	}
	cap.PythonGpuCapable = pythonGpuWheelCapable(cap.PythonVersion)
	// Probe the actual hardware so the UI can offer the in-app installer only on a
	// CUDA-capable host (and never pointlessly reinstall on a GPU-less machine).
	cap.Gpu = detectNvidiaGPU(ctx)
	cap.HasGpu = cap.Gpu != ""
	cap.CanInstall = cap.HasGpu && s.depsScript() != ""

	cap.Available = cap.Python && cap.Ultralytics && strings.TrimSpace(s.trainCfg.TrainScript) != ""
	const setupHint = " Tip: use “Install GPU support” below to set up the right dependencies automatically, then restart."
	switch {
	case !cap.Ultralytics:
		cap.Detail = "ultralytics is not installed — install it to train in-app, or export and train elsewhere." + setupHint
	case !cap.Torch:
		cap.Detail = "PyTorch is not installed in this Python environment." + setupHint
	case cap.Cuda:
		cap.Detail = "Ready — CUDA GPU detected" + nonEmptyPrefix(": ", cap.Device) + "."
	case cap.HasGpu && !cap.PythonGpuCapable:
		// The classic trap: a brand-new Python (e.g. 3.14) PyTorch has no CUDA wheels
		// for, so only the CPU build can install no matter the GPU. The installer can
		// fetch a supported Python 3.13 and switch to it — via winget on Windows, via
		// pyenv on Linux (which needs build tools; it falls back to instructions).
		tool := "winget"
		if runtime.GOOS != "windows" {
			tool = "pyenv"
		}
		cap.Detail = fmt.Sprintf("Python %s has no PyTorch CUDA wheels (only 3.9–3.13 do), so your %s can't be used. “Install GPU support” will set up a supported Python 3.13 (via %s) and switch the app to it.", cap.PythonVersion, cap.Gpu, tool)
	case cudaBuild == "":
		cap.Detail = fmt.Sprintf("PyTorch is the CPU-only build (%s) — your NVIDIA GPU will NOT be used, and CPU training is impractically slow.%s", cap.TorchVersion, setupHint)
	default:
		cap.Detail = fmt.Sprintf("PyTorch %s has CUDA %s support but can't see a GPU — check the NVIDIA driver. CPU training is impractically slow.", cap.TorchVersion, cudaBuild)
	}
	return cap
}

// pythonGpuWheelCapable reports whether a "3.x" version string is one PyTorch
// ships CUDA wheels for (CPython 3.9–3.13). Unknown/empty versions are treated as
// capable so we never block on a probe that simply didn't report a version.
func pythonGpuWheelCapable(version string) bool {
	version = strings.TrimSpace(version)
	if version == "" {
		return true
	}
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return true
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return true
	}
	return major == 3 && minor >= 9 && minor <= 13
}

func nonEmptyPrefix(prefix, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return prefix + value
}

// detectNvidiaGPU returns the first NVIDIA GPU name reported by nvidia-smi, or ""
// if the tool is absent or no GPU/driver is present. This is the "is the hardware
// CUDA-capable" gate for the in-app installer.
func detectNvidiaGPU(ctx context.Context) string {
	probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, "nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			return name
		}
	}
	return ""
}

// depsScript returns the absolute path to the platform setup script (next to the
// train worker), or "" if it can't be located.
func (s *trainingService) depsScript() string {
	trainScript := strings.TrimSpace(s.trainCfg.TrainScript)
	if trainScript == "" {
		return ""
	}
	name := "setup.sh"
	if runtime.GOOS == "windows" {
		name = "setup.ps1"
	}
	script := filepath.Join(filepath.Dir(trainScript), name)
	if !fileExists(script) {
		return ""
	}
	abs, err := filepath.Abs(script)
	if err != nil {
		return script
	}
	return abs
}

// DepsSetupState reports the live status of an in-app dependency install.
type DepsSetupState struct {
	Running bool   `json:"running"`
	Status  string `json:"status"` // "", running, done, failed
	Log     string `json:"log"`
}

// DepsSetupStatus returns the current installer state for polling.
func (s *trainingService) DepsSetupStatus() DepsSetupState {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	return DepsSetupState{Running: s.setupRunning, Status: s.setupStatus, Log: s.setupLog}
}

// StartDepsSetup verifies the hardware is CUDA-capable, then runs the platform
// setup script in the background (pausing the detector so PyTorch files aren't
// locked). One install at a time. The frontend polls DepsSetupStatus to refresh.
func (s *trainingService) StartDepsSetup(ctx context.Context) error {
	script := s.depsScript()
	if script == "" {
		return errors.New("the dependency setup script is not available on this host")
	}
	if detectNvidiaGPU(ctx) == "" {
		return errors.New("no CUDA-capable NVIDIA GPU detected — GPU training is not available on this machine")
	}

	s.setupMu.Lock()
	if s.setupRunning {
		s.setupMu.Unlock()
		return errors.New("a dependency install is already in progress")
	}
	s.setupRunning = true
	s.setupStatus = "running"
	s.setupLog = "Starting GPU dependency install — this can take several minutes (downloading PyTorch)...\n"
	s.setupMu.Unlock()

	go s.runDepsSetup(script)
	return nil
}

// runDepsSetup executes the setup script, streaming output to the live log and
// pausing the detector around the install so it releases the model files.
func (s *trainingService) runDepsSetup(script string) {
	defer func() {
		s.setupMu.Lock()
		s.setupRunning = false
		s.setupMu.Unlock()
	}()

	// Pause the worker so pip can replace torch's DLLs (Windows file locks).
	if p, ok := s.detector.(interface {
		Pause()
		Resume()
	}); ok {
		p.Pause()
		defer p.Resume()
	}

	py := strings.TrimSpace(s.trainCfg.PythonCmd)
	if py == "" {
		py = "python"
	}
	var runner string
	var args []string
	if runtime.GOOS == "windows" {
		runner = "powershell"
		args = []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script, "-Python", py}
	} else {
		runner = "bash"
		args = []string{script, py}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, runner, args...)
	writer := &setupLogWriter{s: s}
	cmd.Stdout = writer
	cmd.Stderr = writer

	err := cmd.Run()
	s.setupMu.Lock()
	if err != nil {
		s.setupStatus = "failed"
		s.setupLog += "\n[failed] " + err.Error() + "\n"
		s.setupMu.Unlock()
		return
	}
	s.setupStatus = "done"
	// If the installer switched to a different (GPU-capable) Python, repoint the
	// app config at it so the detector uses it after the next restart.
	chosen := parsePythonMarker(s.setupLog)
	s.setupMu.Unlock()

	if chosen != "" && !strings.EqualFold(strings.TrimSpace(chosen), strings.TrimSpace(s.trainCfg.PythonCmd)) {
		note := ""
		if err := s.repointDetectorPython(chosen); err != nil {
			note = fmt.Sprintf("[warn] installed into %s but could not update config.json automatically (%v). Set vision.detector.command to that path manually.\n", chosen, err)
		} else {
			note = fmt.Sprintf("[done] Switched the app's Python to %s and updated config.json. Restart the server to use the GPU.\n", chosen)
		}
		s.setupMu.Lock()
		s.setupLog += "\n" + note
		s.setupMu.Unlock()
		return
	}

	s.setupMu.Lock()
	s.setupLog += "\n[done] Dependencies installed. Restart the server to apply.\n"
	s.setupMu.Unlock()
}

// parsePythonMarker returns the last interpreter path the installer announced via
// a "MYMATASAN_PYTHON=<path>" line, or "" if it did not switch interpreters.
func parsePythonMarker(log string) string {
	const marker = "MYMATASAN_PYTHON="
	found := ""
	for _, line := range strings.Split(log, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, marker) {
			found = strings.TrimSpace(strings.TrimPrefix(line, marker))
		}
	}
	return found
}

// repointDetectorPython rewrites vision.detector.command in the app config.json to
// pythonPath, touching only that one value so the rest of the file (ordering,
// other settings) is preserved.
func (s *trainingService) repointDetectorPython(pythonPath string) error {
	configFile := strings.TrimSpace(s.trainCfg.ConfigFile)
	if configFile == "" {
		return errors.New("config file path is unknown")
	}
	data, err := os.ReadFile(configFile)
	if err != nil {
		return err
	}
	// Match the "command": "..." value inside the "detector" object. command
	// appears before any nested object (e.g. classMap), so the non-greedy run
	// never crosses a closing brace.
	re := regexp.MustCompile(`("detector"\s*:\s*\{[^}]*?"command"\s*:\s*")([^"]*)(")`)
	loc := re.FindSubmatchIndex(data)
	if loc == nil {
		return errors.New("could not locate vision.detector.command")
	}
	// Group 2 (loc[4]:loc[5]) is the current command value; JSON-escape the new one.
	encoded, _ := json.Marshal(pythonPath)
	escaped := encoded[1 : len(encoded)-1] // strip surrounding quotes
	out := make([]byte, 0, len(data)-(loc[5]-loc[4])+len(escaped))
	out = append(out, data[:loc[4]]...)
	out = append(out, escaped...)
	out = append(out, data[loc[5]:]...)
	return os.WriteFile(configFile, out, 0o644)
}

// setupLogWriter appends installer output to the service's live log, capped to
// the last ~12KB so the polled payload stays small.
type setupLogWriter struct {
	s *trainingService
}

func (w *setupLogWriter) Write(p []byte) (int, error) {
	w.s.setupMu.Lock()
	w.s.setupLog += string(p)
	if len(w.s.setupLog) > 12000 {
		w.s.setupLog = "...\n" + w.s.setupLog[len(w.s.setupLog)-12000:]
	}
	w.s.setupMu.Unlock()
	return len(p), nil
}

// StartTraining exports the dataset and launches a background training run,
// returning the pending model immediately. Only one run is allowed at a time.
func (s *trainingService) StartTraining(ctx context.Context, req StartTrainingRequest, userId int64) (*entities.TrainingModel, error) {
	if strings.TrimSpace(s.trainCfg.TrainScript) == "" || strings.TrimSpace(s.trainCfg.PythonCmd) == "" {
		return nil, errors.New("in-app training is not configured on this host")
	}

	s.trainMu.Lock()
	if s.training {
		s.trainMu.Unlock()
		return nil, errors.New("a training run is already in progress")
	}
	s.training = true
	s.trainMu.Unlock()
	release := func() {
		s.trainMu.Lock()
		s.training = false
		s.trainMu.Unlock()
	}

	dataset, err := s.datasets.GetById(ctx, "", uint64(req.DatasetId))
	if err != nil {
		release()
		return nil, fmt.Errorf("dataset not found: %w", err)
	}
	exportDir, names, err := s.buildExportDir(ctx, req.DatasetId)
	if err != nil {
		release()
		return nil, err
	}

	epochs := req.Epochs
	if epochs <= 0 {
		epochs = 50
	}
	imgsz := req.Imgsz
	if imgsz <= 0 {
		imgsz = 640
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = fmt.Sprintf("%s %s", dataset.Name, time.Now().Format("2006-01-02 15:04"))
	}

	now := s.now().UTC().Unix()
	row := entities.TrainingModel{
		Name:      name,
		DatasetId: req.DatasetId,
		BaseModel: s.trainCfg.BaseModel,
		Epochs:    epochs,
		Imgsz:     imgsz,
		Status:    "pending",
		Progress:  0,
		Classes:   string(mustJSON(names)),
		CreatedBy: userId,
		CreatedAt: now,
		UpdatedBy: userId,
		UpdatedAt: now,
	}
	id, err := s.models.Create(ctx, modelTable, row)
	if err != nil {
		release()
		return nil, err
	}
	row.Id = int64(id)

	go func() {
		defer release()
		s.runTraining(context.Background(), row, exportDir)
	}()
	return &row, nil
}

// runTraining launches the train worker, parses its JSON progress, and records
// the result on the model row. It runs in its own goroutine.
func (s *trainingService) runTraining(ctx context.Context, model entities.TrainingModel, exportDir string) {
	// Absolute project dir so ultralytics writes where we expect regardless of the
	// Python process cwd, and so the reported weights path resolves for the copy.
	modelDir, absErr := filepath.Abs(filepath.Join(s.modelsDir(), fmt.Sprintf("%d", model.Id)))
	if absErr != nil {
		modelDir = filepath.Join(s.modelsDir(), fmt.Sprintf("%d", model.Id))
	}
	_ = os.MkdirAll(modelDir, 0o755)
	s.setModelProgress(model.Id, "running", 0, "")

	base := strings.TrimSpace(model.BaseModel)
	if base == "" {
		base = "yolo11n.pt"
	}
	args := []string{
		s.trainCfg.TrainScript,
		"--data", filepath.Join(exportDir, "data.yaml"),
		"--base", base,
		"--epochs", strconv.Itoa(model.Epochs),
		"--imgsz", strconv.Itoa(model.Imgsz),
		"--project", modelDir,
		"--name", "run",
	}
	cmd := exec.CommandContext(ctx, s.trainCfg.PythonCmd, args...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.failModel(model.Id, err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		s.failModel(model.Id, err.Error())
		return
	}

	var weights, failMsg string
	var metrics map[string]float64
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var ev struct {
			Event   string             `json:"event"`
			Epoch   int                `json:"epoch"`
			Epochs  int                `json:"epochs"`
			Weights string             `json:"weights"`
			Metrics map[string]float64 `json:"metrics"`
			Message string             `json:"message"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(scanner.Text())), &ev) != nil {
			continue
		}
		switch ev.Event {
		case "progress":
			pct := 0
			if ev.Epochs > 0 {
				pct = ev.Epoch * 100 / ev.Epochs
			}
			s.setModelProgress(model.Id, "running", pct, "")
		case "completed":
			weights = ev.Weights
			metrics = ev.Metrics
		case "error":
			failMsg = ev.Message
		}
	}
	waitErr := cmd.Wait()
	if failMsg != "" {
		s.failModel(model.Id, failMsg)
		return
	}
	if waitErr != nil {
		s.failModel(model.Id, "training process failed: "+waitErr.Error())
		return
	}
	// If the reported path is missing, search the run dir for best.pt as a fallback.
	if weights == "" || !fileExists(weights) {
		weights = findFile(modelDir, "best.pt")
	}
	if weights == "" {
		s.failModel(model.Id, "training finished but best.pt was not found")
		return
	}

	// Copy best.pt to the canonical model path so activation/import behave the same.
	dest := filepath.Join(modelDir, "best.pt")
	if sameFile(weights, dest) {
		s.completeModel(model.Id, dest, string(mustJSON(metrics)))
		return
	}
	if err := copyFile(weights, dest); err != nil {
		s.failModel(model.Id, "could not store weights: "+err.Error())
		return
	}
	s.completeModel(model.Id, dest, string(mustJSON(metrics)))
}

func (s *trainingService) setModelProgress(id int64, status string, progress int, metrics string) {
	model, err := s.models.GetById(context.Background(), modelTable, uint64(id))
	if err != nil {
		return
	}
	model.Status = status
	model.Progress = progress
	if metrics != "" {
		model.Metrics = metrics
	}
	model.UpdatedAt = s.now().UTC().Unix()
	_, _ = s.models.UpdateById(context.Background(), modelTable, *model)
}

func (s *trainingService) failModel(id int64, message string) {
	s.setModelProgress(id, "failed", 0, string(mustJSON(map[string]string{"error": message})))
}

func (s *trainingService) completeModel(id int64, filePath, metrics string) {
	model, err := s.models.GetById(context.Background(), modelTable, uint64(id))
	if err != nil {
		return
	}
	model.Status = "completed"
	model.Progress = 100
	model.FilePath = filePath
	if metrics != "" && metrics != "null" {
		model.Metrics = metrics
	}
	model.UpdatedAt = s.now().UTC().Unix()
	_, _ = s.models.UpdateById(context.Background(), modelTable, *model)
}

// stockModelOptions are the downloadable base models (ultralytics fetches them
// by name). Ordered fastest/least-accurate → slowest/most-accurate.
var stockModelOptions = []string{"yolo11n.pt", "yolo11s.pt", "yolo11m.pt", "yolo11l.pt", "yolo11x.pt"}

// StockModelInfo is the current base model + the selectable options.
type StockModelInfo struct {
	Current string   `json:"current"`
	Options []string `json:"options"`
}

func (s *trainingService) GetStockModel(ctx context.Context) StockModelInfo {
	current := "yolo11n.pt"
	if s.stockModelFile != "" {
		if data, err := os.ReadFile(s.stockModelFile); err == nil {
			if v := strings.TrimSpace(string(data)); v != "" {
				current = filepath.Base(v)
			}
		}
	}
	return StockModelInfo{Current: current, Options: stockModelOptions}
}

// SetStockModel changes the always-on base model. A known name (yolo11s/m/l/x) is
// downloaded via ultralytics; a custom value is treated as a local .pt path.
// yolo11n / empty reverts to the bundled default. The worker is reloaded to apply.
func (s *trainingService) SetStockModel(ctx context.Context, model string, userId int64) error {
	if s.stockModelFile == "" {
		return errors.New("stock model is not configurable on this host")
	}
	model = strings.TrimSpace(model)
	if model == "" || strings.EqualFold(model, "yolo11n.pt") || strings.EqualFold(model, "default") {
		_ = os.Remove(s.stockModelFile)
		s.reloadDetector()
		return nil
	}

	resolved := ""
	known := false
	for _, opt := range stockModelOptions {
		if strings.EqualFold(opt, model) {
			known = true
			break
		}
	}
	if known {
		path, err := s.downloadStockModel(strings.ToLower(model))
		if err != nil {
			return err
		}
		resolved = path
	} else {
		if _, err := os.Stat(model); err != nil {
			return fmt.Errorf("model file not found: %s", model)
		}
		resolved, _ = filepath.Abs(model)
	}

	if err := os.MkdirAll(filepath.Dir(s.stockModelFile), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(s.stockModelFile, []byte(resolved), 0o644); err != nil {
		return err
	}
	s.reloadDetector()
	return nil
}

// downloadStockModel fetches a known base model via ultralytics and returns its
// local path. The download can take a while (large variants), so it runs with a
// generous independent timeout rather than the request context.
func (s *trainingService) downloadStockModel(name string) (string, error) {
	if strings.TrimSpace(s.trainCfg.PythonCmd) == "" {
		return "", errors.New("python is not configured, cannot download a model")
	}
	dir := filepath.Join(s.dataDir, "stock")
	_ = os.MkdirAll(dir, 0o755)
	script := "import sys, contextlib\n" +
		"from ultralytics import YOLO\n" +
		"with contextlib.redirect_stdout(sys.stderr):\n" +
		" m = YOLO('" + name + "')\n" +
		"print('CKPT:' + (getattr(m, 'ckpt_path', '') or ''))\n"
	dlCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(dlCtx, s.trainCfg.PythonCmd, "-c", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("download failed: %v", err)
	}
	// Prefer the file in our stock dir; else the path ultralytics reported.
	if local := filepath.Join(dir, name); fileExists(local) {
		abs, _ := filepath.Abs(local)
		return abs, nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CKPT:") {
			p := strings.TrimSpace(strings.TrimPrefix(line, "CKPT:"))
			if p != "" && fileExists(p) {
				abs, _ := filepath.Abs(p)
				return abs, nil
			}
		}
	}
	return "", errors.New("download finished but the model file was not found")
}

// readModelLabels loads a YOLO .pt via ultralytics and returns its embedded
// class names (model.names), in class-index order, lowercased. These are the
// exact raw labels the worker will emit, so importing can auto-register them
// instead of relying on the user typing matching strings. Best-effort: returns
// an error if Python/ultralytics is unavailable or the file can't be read, in
// which case the caller falls back to any user-supplied classes.
func (s *trainingService) readModelLabels(weightsPath string) ([]string, error) {
	py := strings.TrimSpace(s.trainCfg.PythonCmd)
	if py == "" {
		return nil, errors.New("python is not configured")
	}
	// Load under redirect_stdout(stderr) to swallow ultralytics' banner; the
	// final marker line prints to the restored real stdout. The weights path is
	// passed as argv to avoid quoting Windows backslashes into the script.
	script := "import sys, json, contextlib\n" +
		"with contextlib.redirect_stdout(sys.stderr):\n" +
		" from ultralytics import YOLO\n" +
		" names = YOLO(sys.argv[1]).names\n" +
		"vals = [names[k] for k in sorted(names)] if isinstance(names, dict) else list(names)\n" +
		"print('LABELS:' + json.dumps([str(v).strip().lower() for v in vals]))\n"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, py, "-c", script, weightsPath).Output()
	if err != nil {
		return nil, fmt.Errorf("could not read model labels: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "LABELS:") {
			continue
		}
		var labels []string
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "LABELS:")), &labels); err != nil {
			return nil, fmt.Errorf("could not parse model labels: %v", err)
		}
		return labels, nil
	}
	return nil, errors.New("model labels not found in worker output")
}

func (s *trainingService) reloadDetector() {
	if r, ok := s.detector.(interface{ Reload() error }); ok {
		_ = r.Reload()
	}
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// sameFile reports whether two paths resolve to the same file on disk.
func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// findFile walks dir for the first file with the given base name.
func findFile(dir, name string) string {
	found := ""
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !info.IsDir() && info.Name() == name {
			found = path
		}
		return nil
	})
	return found
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
