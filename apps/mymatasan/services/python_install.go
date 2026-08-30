package services

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/procutil"
)

// Pinned astral python-build-standalone build. `install_only` tarballs extract to a
// self-contained python/ dir (no system Python needed). Bump both together.
const (
	pyStandaloneVersion = "3.12.7"
	pyStandaloneRelease = "20241016"
	// PyTorch wheel indexes: a CUDA build when an NVIDIA GPU is present (cu128 covers
	// Blackwell/RTX 50-series and older with a recent driver), else the small CPU build.
	torchIndexCUDA = "https://download.pytorch.org/whl/cu128"
	torchIndexCPU  = "https://download.pytorch.org/whl/cpu"
)

// PythonRuntimeStatus reports whether a usable AI Python runtime (Python + ultralytics
// + torch) is available, and where.
type PythonRuntimeStatus struct {
	Found      bool   `json:"found"`
	Python     string `json:"python"`
	Torch      string `json:"torch,omitempty"`
	CUDA       bool   `json:"cuda"`
	Ultralytic bool   `json:"ultralytics"`
}

// PythonInstallState is the live status of a background AI-runtime install, polled by
// the UI (same shape as the ffmpeg installer).
type PythonInstallState struct {
	Running   bool   `json:"running"`
	Status    string `json:"status"` // "", running, done, failed
	Log       string `json:"log"`
	Python    string `json:"python"`
	Supported bool   `json:"supported"`
}

// PythonInstaller downloads a self-contained Python for the host, pip-installs the YOLO
// AI stack (torch — GPU build when an NVIDIA GPU is present, else CPU — plus
// ultralytics), and persists the resolved interpreter into vision.detector.command so
// the detector uses it after a restart. One install runs at a time; the UI polls status.
type PythonInstaller struct {
	dataDir    string
	configPath string
	client     *http.Client

	mu      sync.Mutex
	running bool
	status  string
	logTxt  string
	python  string
}

// NewPythonInstaller builds the installer. dataDir is the writable state root (the
// runtime lands under dataDir/pyruntime); configPath is the app config the resolved
// interpreter path is written back to.
func NewPythonInstaller(dataDir, configPath string) *PythonInstaller {
	return &PythonInstaller{
		dataDir:    dataDir,
		configPath: configPath,
		client:     &http.Client{Timeout: 30 * time.Minute},
	}
}

func (p *PythonInstaller) pythonExe() string {
	rel := filepath.Join("python", "bin", "python3")
	if runtime.GOOS == "windows" {
		rel = filepath.Join("python", "python.exe")
	}
	return filepath.Join(p.dataDir, "pyruntime", rel)
}

// pythonProbeScript prints "<torch-version> <cuda-bool> <has-ultralytics>" for an
// interpreter, so one run tells us whether the AI stack is usable.
const pythonProbeScript = "import importlib.util as u;\n" +
	"tv=''; cu='False'; ul='False'\n" +
	"try:\n import torch; tv=torch.__version__; cu=str(torch.cuda.is_available())\n" +
	"except Exception: pass\n" +
	"ul=str(u.find_spec('ultralytics') is not None)\n" +
	"print(tv, cu, ul)"

// PythonStatus reports the AI runtime the app will actually use. It probes candidates in
// priority order — the configured detector command (vision.detector.command, i.e. what
// really runs detection, which may be a user-supplied system Python with a GPU torch
// build) first, then the app's own bundled runtime under dataDir/pyruntime — and returns
// the first that both exists and has ultralytics. This stops a "bring your own Python"
// setup from being falsely reported as "not installed" just because the bundled runtime
// was never downloaded. If a candidate exists but lacks the packages, that is reported
// (found, but ultralytics/torch missing) rather than a bare "not found".
func (p *PythonInstaller) PythonStatus(ctx context.Context) PythonRuntimeStatus {
	candidates := make([]string, 0, 2)
	if cmd := strings.TrimSpace(p.detectorCommand()); cmd != "" {
		candidates = append(candidates, cmd)
	}
	if bundled := p.pythonExe(); !containsString(candidates, bundled) {
		candidates = append(candidates, bundled)
	}

	var firstFound PythonRuntimeStatus
	for _, exe := range candidates {
		st := p.probeInterpreter(ctx, exe)
		if !st.Found {
			continue
		}
		if st.Ultralytic {
			return st // a usable runtime (has ultralytics) — report it
		}
		if !firstFound.Found {
			firstFound = st // present but incomplete; keep as fallback report
		}
	}
	return firstFound
}

// probeInterpreter checks that exe exists (resolving a bare command via PATH) and runs
// the torch/ultralytics probe. A missing interpreter returns a zero (not-found) status.
func (p *PythonInstaller) probeInterpreter(ctx context.Context, exe string) PythonRuntimeStatus {
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return PythonRuntimeStatus{}
	}
	resolved := exe
	if filepath.IsAbs(exe) {
		if _, err := os.Stat(exe); err != nil {
			return PythonRuntimeStatus{}
		}
	} else if lp, err := exec.LookPath(exe); err == nil {
		resolved = lp
	} else {
		return PythonRuntimeStatus{}
	}
	res := PythonRuntimeStatus{Found: true, Python: resolved}
	out, err := runProbe(ctx, resolved, []string{"-c", pythonProbeScript}, 20*time.Second)
	if err == nil {
		fields := strings.Fields(strings.TrimSpace(out))
		if len(fields) == 3 {
			res.Torch = fields[0]
			res.CUDA = fields[1] == "True"
			res.Ultralytic = fields[2] == "True"
		}
	}
	return res
}

// detectorCommand reads vision.detector.command from the app config (the interpreter the
// detector runs with), or "" when unset/unreadable.
func (p *PythonInstaller) detectorCommand() string {
	return DetectorCommandFromConfig(p.configPath)
}

// DetectorCommandFromConfig reads vision.detector.command straight from the config FILE rather than
// the boot-time struct, so a caller sees an interpreter this installer (or the operator) repointed
// after startup. Returns "" when there is nothing to read. Shared with the face-model installer,
// which has the same "which Python will actually run this?" question.
func DetectorCommandFromConfig(configPath string) string {
	if strings.TrimSpace(configPath) == "" {
		return ""
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	var cfg map[string]any
	if json.Unmarshal(data, &cfg) != nil {
		return ""
	}
	vision, _ := cfg["vision"].(map[string]any)
	if vision == nil {
		return ""
	}
	detector, _ := vision["detector"].(map[string]any)
	if detector == nil {
		return ""
	}
	cmd, _ := detector["command"].(string)
	return cmd
}

// InstallStatus returns the current install state for polling.
func (p *PythonInstaller) InstallStatus() PythonInstallState {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _, err := pythonDownloadURL(runtime.GOOS, runtime.GOARCH)
	return PythonInstallState{
		Running:   p.running,
		Status:    p.status,
		Log:       p.logTxt,
		Python:    p.python,
		Supported: err == nil,
	}
}

// StartInstall begins a background download + pip install. It errors immediately when
// unsupported for the OS/arch or one is already running.
func (p *PythonInstaller) StartInstall(_ context.Context) error {
	url, exeRel, err := pythonDownloadURL(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return errors.New("an AI runtime install is already in progress")
	}
	p.running = true
	p.status = "running"
	p.logTxt = "Starting AI runtime install…\n"
	p.python = ""
	p.mu.Unlock()

	go p.run(url, exeRel)
	return nil
}

func (p *PythonInstaller) run(url, exeRel string) {
	defer func() {
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
	}()

	installDir := filepath.Join(p.dataDir, "pyruntime")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		p.fail(fmt.Sprintf("create install dir: %v", err))
		return
	}
	// Fresh extract each run so a retry can't mix versions.
	_ = os.RemoveAll(filepath.Join(installDir, "python"))

	tmp, err := os.MkdirTemp("", "pyrt-")
	if err != nil {
		p.fail(fmt.Sprintf("temp dir: %v", err))
		return
	}
	defer os.RemoveAll(tmp)

	archive := filepath.Join(tmp, "python.tar.gz")
	p.appendLog(fmt.Sprintf("Downloading Python %s (%s)…\n", pyStandaloneVersion, url))
	if err := p.download(url, archive); err != nil {
		p.fail(fmt.Sprintf("download failed: %v", err))
		return
	}

	p.appendLog("Extracting Python…\n")
	if err := extractTarGz(archive, installDir); err != nil {
		p.fail(fmt.Sprintf("extract failed: %v", err))
		return
	}
	python := filepath.Join(installDir, exeRel)
	if _, err := os.Stat(python); err != nil {
		p.fail(fmt.Sprintf("python not found after extract at %s", python))
		return
	}

	// GPU-aware torch index.
	gpu := hasNvidiaGPU()
	torchIndex := torchIndexCPU
	if gpu {
		torchIndex = torchIndexCUDA
		p.appendLog("NVIDIA GPU detected — installing the CUDA build of PyTorch (large download).\n")
	} else {
		p.appendLog("No NVIDIA GPU detected — installing the CPU build of PyTorch.\n")
	}

	steps := [][]string{
		{"-m", "pip", "install", "--disable-pip-version-check", "--no-warn-script-location", "--upgrade", "pip"},
		{"-m", "pip", "install", "--disable-pip-version-check", "--no-warn-script-location", "torch", "torchvision", "--index-url", torchIndex},
		{"-m", "pip", "install", "--disable-pip-version-check", "--no-warn-script-location", "ultralytics"},
	}
	for _, args := range steps {
		p.appendLog("\n$ python " + strings.Join(args, " ") + "\n")
		if err := p.runStreaming(python, args); err != nil {
			p.fail(fmt.Sprintf("pip step failed: %v", err))
			return
		}
	}

	p.appendLog("\nVerifying…\n")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := runProbe(ctx, python, []string{"-c", "import torch, ultralytics; print('torch', torch.__version__, 'cuda', torch.cuda.is_available())"}, 60*time.Second)
	if err != nil {
		p.fail(fmt.Sprintf("the installed runtime did not import torch/ultralytics: %v", err))
		return
	}
	p.appendLog(strings.TrimSpace(out) + "\n")

	if err := p.persistCommand(python); err != nil {
		p.fail(fmt.Sprintf("AI runtime installed but saving the detector command failed: %v", err))
		return
	}

	p.mu.Lock()
	p.status = "done"
	p.python = python
	p.logTxt += "\nDone. AI runtime installed at " + python + "\nRestart the app to enable AI detection.\n"
	p.mu.Unlock()
}

// persistCommand writes the interpreter into vision.detector.command in the app config,
// preserving every other field (read-modify-write as a generic map).
func (p *PythonInstaller) persistCommand(python string) error {
	if strings.TrimSpace(p.configPath) == "" {
		return errors.New("no config path configured")
	}
	data, err := os.ReadFile(p.configPath)
	if err != nil {
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	vision, _ := cfg["vision"].(map[string]any)
	if vision == nil {
		vision = map[string]any{}
		cfg["vision"] = vision
	}
	detector, _ := vision["detector"].(map[string]any)
	if detector == nil {
		detector = map[string]any{}
		vision["detector"] = detector
	}
	detector["command"] = python
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.configPath, append(out, '\n'), 0o644)
}

func (p *PythonInstaller) download(url, dest string) error {
	resp, err := p.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned %s", resp.Status)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

// runStreaming runs the interpreter with args, piping combined output into the live log
// so the UI shows pip progress.
func (p *PythonInstaller) runStreaming(python string, args []string) error {
	cmd := exec.Command(python, args...)
	procutil.HideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout // merge stderr into the same stream
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		p.appendLog(scanner.Text() + "\n")
	}
	return cmd.Wait()
}

func (p *PythonInstaller) appendLog(line string) {
	p.mu.Lock()
	p.logTxt += line
	p.mu.Unlock()
}

func (p *PythonInstaller) fail(msg string) {
	p.mu.Lock()
	p.status = "failed"
	p.logTxt += "[failed] " + msg + "\n"
	p.mu.Unlock()
}

// pythonDownloadURL returns the astral python-build-standalone install_only URL and the
// interpreter path inside the archive for the OS/arch, or an error when unsupported.
// Pure (no I/O) so the matrix is unit-testable.
func pythonDownloadURL(goos, goarch string) (url string, exeRel string, err error) {
	triple := ""
	switch goos + "/" + goarch {
	case "windows/amd64":
		triple = "x86_64-pc-windows-msvc"
	case "windows/arm64":
		triple = "aarch64-pc-windows-msvc"
	case "linux/amd64":
		triple = "x86_64-unknown-linux-gnu"
	case "linux/arm64":
		triple = "aarch64-unknown-linux-gnu"
	default:
		return "", "", fmt.Errorf("no standalone Python build for %s/%s", goos, goarch)
	}
	url = fmt.Sprintf(
		"https://github.com/astral-sh/python-build-standalone/releases/download/%s/cpython-%s+%s-%s-install_only.tar.gz",
		pyStandaloneRelease, pyStandaloneVersion, pyStandaloneRelease, triple,
	)
	exeRel = filepath.Join("python", "bin", "python3")
	if goos == "windows" {
		exeRel = filepath.Join("python", "python.exe")
	}
	return url, exeRel, nil
}

// hasNvidiaGPU reports whether an NVIDIA GPU is usable (nvidia-smi runs successfully).
func hasNvidiaGPU() bool {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, "nvidia-smi", "-L")
	procutil.HideWindow(probe)
	return probe.Run() == nil
}

// runProbe runs a short command and returns its combined output.
func runProbe(ctx context.Context, exe string, args []string, timeout time.Duration) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, exe, args...)
	procutil.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// extractTarGz extracts a .tar.gz into destDir (paths are validated to stay within it).
func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	destClean := filepath.Clean(destDir)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, filepath.FromSlash(hdr.Name))
		if !strings.HasPrefix(filepath.Clean(target), destClean+string(os.PathSeparator)) && filepath.Clean(target) != destClean {
			return fmt.Errorf("tar entry escapes dest: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)|0o200)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			// Standalone Python ships symlinks (e.g. python3 -> python3.12). Recreate
			// them; ignore failures on platforms/filesystems that disallow symlinks.
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = os.Remove(target)
			_ = os.Symlink(hdr.Linkname, target)
		}
	}
	return nil
}
