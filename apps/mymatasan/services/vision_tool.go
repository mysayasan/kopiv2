package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/infra/externaltools"
	"github.com/mysayasan/kopiv2/infra/vision"
)

type VisionToolSettings struct {
	Mode              string
	Command           string
	Args              []string
	TimeoutMs         int
	UseMotionFallback bool
}

// PackageInstallHint describes how to install a missing Python package.
type PackageInstallHint struct {
	ImportName string `json:"importName"`
	PipName    string `json:"pipName"`
	Command    string `json:"command,omitempty"`
	Manual     bool   `json:"manual"`
	Note       string `json:"note,omitempty"`
}

type VisionToolStatus struct {
	Mode              string               `json:"mode"`
	Required          bool                 `json:"required"`
	Available         bool                 `json:"available"`
	NativeFallback    bool                 `json:"nativeFallback"`
	CommandFound      bool                 `json:"commandFound"`
	CommandPath       string               `json:"commandPath"`
	CommandSource     string               `json:"commandSource"`
	PythonVersion     string               `json:"pythonVersion"`
	PackagesAvailable bool                 `json:"packagesAvailable"`
	PackageError      string               `json:"packageError"`
	MissingPackages   []string             `json:"missingPackages,omitempty"`
	InstallHints      []PackageInstallHint `json:"installHints,omitempty"`
	TrackerAvailable  bool                 `json:"trackerAvailable"`
	WorkerPath        string               `json:"workerPath"`
	WorkerFound       bool                 `json:"workerFound"`
	ModelPath         string               `json:"modelPath"`
	ModelFound        bool                 `json:"modelFound"`
	Summary           string               `json:"summary"`
	Observations      []string             `json:"observations"`
}

func CheckVisionTool(ctx context.Context, settings VisionToolSettings) VisionToolStatus {
	mode := strings.ToLower(strings.TrimSpace(settings.Mode))
	if mode == "" {
		mode = vision.DetectorModeMotion
	}
	status := VisionToolStatus{
		Mode:           mode,
		NativeFallback: settings.UseMotionFallback,
	}
	if mode == vision.DetectorModeMotion {
		status.Available = true
		status.NativeFallback = true
		status.Summary = "Native motion detector is active; external AI tools are not required."
		status.Observations = []string{"Person, vehicle, animal, fire, and smoke labels require an external AI detector. Motion and motion-based line crossing can run natively."}
		return status
	}

	status.Required = true
	status.Observations = append(status.Observations, "Configured detector mode: "+mode)
	command := strings.TrimSpace(settings.Command)
	if command == "" {
		status.Summary = visionToolUnavailableSummary(status.NativeFallback)
		status.Observations = append(status.Observations, "AI detector command is empty.")
		return status
	}

	tool := externaltools.CheckExecutable(ctx, externaltools.ExecutableSpec{
		Name:           "AI detector command",
		ConfiguredPath: command,
		ExecutableName: command,
		ProbeArgs:      []string{"--version"},
		Timeout:        visionToolTimeout(settings.TimeoutMs),
		MaxOutputBytes: 2048,
	})
	status.CommandFound = tool.Found
	status.CommandPath = tool.Path
	status.CommandSource = tool.Source
	if tool.Error != "" {
		status.Observations = append(status.Observations, "Command probe warning: "+tool.Error)
	}
	if !tool.Found {
		status.Summary = visionToolUnavailableSummary(status.NativeFallback)
		status.Observations = append(status.Observations, "AI command was not found; install the tool or change the configured command path.")
		return status
	}
	status.Observations = append(status.Observations, "AI command resolved from "+tool.Source+": "+tool.Path)

	status.WorkerPath = firstScriptArg(settings.Args)
	if status.WorkerPath != "" {
		if path, found := resolveExistingPath(status.WorkerPath); found {
			status.WorkerPath = path
			status.WorkerFound = true
			status.Observations = append(status.Observations, "Worker script found: "+path)
		} else {
			status.Observations = append(status.Observations, "Worker script was not found: "+status.WorkerPath)
		}
	} else {
		status.WorkerFound = true
	}

	if isPythonCommand(command, status.WorkerPath) {
		status.PythonVersion = probePythonVersion(ctx, tool.Path, settings.TimeoutMs)
		var missingPkgs []string
		status.PackagesAvailable, status.PackageError, missingPkgs = probePythonPackages(ctx, tool.Path, settings.TimeoutMs)
		if len(missingPkgs) > 0 {
			status.MissingPackages = missingPkgs
			status.InstallHints = packageInstallHints(missingPkgs, tool.Path)
		}
		if status.PythonVersion != "" {
			status.Observations = append(status.Observations, "Python version: "+status.PythonVersion)
		}
		if status.PackagesAvailable {
			status.Observations = append(status.Observations, "Core AI packages ready: ultralytics, cv2, torch.")
		} else {
			status.Observations = append(status.Observations, "Python AI packages are not ready: "+status.PackageError)
		}
		// Tracker package is optional — missing lap/lapx falls back to plain predict (no object ID).
		status.TrackerAvailable = probePythonTracker(ctx, tool.Path, settings.TimeoutMs)
		if status.TrackerAvailable {
			status.Observations = append(status.Observations, "ByteTrack tracker package available (lap or lapx).")
		} else {
			status.Observations = append(status.Observations, "ByteTrack tracker package (lap/lapx) not found — detection works but objects will not have stable track IDs. On ARM/Raspberry Pi install lapx: pip install lapx")
		}
	} else {
		status.PackagesAvailable = true
		status.TrackerAvailable = true
	}

	if strings.EqualFold(filepath.Base(status.WorkerPath), "yolo_worker.py") {
		status.ModelPath = os.Getenv("MYMATASAN_YOLO_MODEL")
		if strings.TrimSpace(status.ModelPath) == "" && status.WorkerPath != "" {
			status.ModelPath = filepath.Join(filepath.Dir(status.WorkerPath), "yolo11n.pt")
		}
		if path, found := resolveExistingPath(status.ModelPath); found {
			status.ModelPath = path
			status.ModelFound = true
			status.Observations = append(status.Observations, "YOLO model found: "+path)
		} else if strings.TrimSpace(status.ModelPath) != "" {
			status.Observations = append(status.Observations, "YOLO model was not found: "+status.ModelPath)
		}
	} else {
		status.ModelFound = true
	}

	status.Available = status.CommandFound && status.PackagesAvailable && status.WorkerFound && status.ModelFound
	if status.Available {
		status.Summary = "AI detector tool is ready."
	} else {
		status.Summary = visionToolUnavailableSummary(status.NativeFallback)
	}
	return status
}

func visionToolTimeout(timeoutMs int) time.Duration {
	if timeoutMs <= 0 {
		return 5 * time.Second
	}
	return time.Duration(timeoutMs) * time.Millisecond
}

func visionToolUnavailableSummary(nativeFallback bool) string {
	if nativeFallback {
		return "AI detector tool is not ready; native fallback remains available."
	}
	return "AI detector tool is not ready."
}

func firstScriptArg(args []string) string {
	for _, arg := range args {
		value := strings.TrimSpace(arg)
		if strings.HasSuffix(strings.ToLower(value), ".py") {
			return value
		}
	}
	if len(args) > 0 {
		return strings.TrimSpace(args[0])
	}
	return ""
}

func resolveExistingPath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	if _, err := os.Stat(path); err != nil {
		return path, false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, true
	}
	return abs, true
}

func isPythonCommand(command string, workerPath string) bool {
	name := strings.ToLower(filepath.Base(strings.TrimSpace(command)))
	name = strings.TrimSuffix(name, ".exe")
	return strings.Contains(name, "python") || strings.HasSuffix(strings.ToLower(workerPath), ".py")
}

func probePythonVersion(ctx context.Context, commandPath string, timeoutMs int) string {
	output, err := externaltools.Probe(ctx, commandPath, []string{"-c", "import sys; print(sys.version.split()[0])"}, visionToolTimeout(timeoutMs), 2048)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(output)
}

// probePythonPackages checks the three core AI packages (ultralytics, cv2, torch).
// The ByteTrack tracker package (lap/lapx) is probed separately by probePythonTracker
// so that it does not block availability on ARM devices where lap has no binary wheels.
func probePythonPackages(ctx context.Context, commandPath string, timeoutMs int) (bool, string, []string) {
	script := "import importlib.util; missing=[m for m in ('ultralytics','cv2','torch') if importlib.util.find_spec(m) is None]; print('ok' if not missing else 'missing:'+','.join(missing))"
	output, err := externaltools.Probe(ctx, commandPath, []string{"-c", script}, visionToolTimeout(timeoutMs), 4096)
	if err != nil {
		if strings.TrimSpace(output) != "" {
			return false, strings.TrimSpace(output), nil
		}
		return false, err.Error(), nil
	}
	output = strings.TrimSpace(output)
	if output == "ok" {
		return true, "", nil
	}
	if output == "" {
		return false, "package probe returned no output", nil
	}
	if strings.HasPrefix(output, "missing:") {
		names := strings.Split(strings.TrimPrefix(output, "missing:"), ",")
		for i, n := range names {
			names[i] = strings.TrimSpace(n)
		}
		return false, output, names
	}
	return false, output, nil
}

// probePythonTracker checks whether a ByteTrack-compatible tracker package is installed.
// It accepts either lap (x86/amd64) or lapx (ARM-compatible alternative with binary wheels).
func probePythonTracker(ctx context.Context, commandPath string, timeoutMs int) bool {
	script := "import importlib.util; print('ok' if any(importlib.util.find_spec(m) for m in ('lap','lapx')) else 'missing')"
	output, err := externaltools.Probe(ctx, commandPath, []string{"-c", script}, visionToolTimeout(timeoutMs), 512)
	return err == nil && strings.TrimSpace(output) == "ok"
}

var packagePipNames = map[string]string{
	"cv2":         "opencv-python",
	"ultralytics": "ultralytics",
	"torch":       "torch",
}

// torch requires a hardware-specific install command; flag it as manual.
var packageManualInstall = map[string]bool{
	"torch": true,
}

var packageNotes = map[string]string{
	"torch": "PyTorch has CPU and GPU variants. Visit https://pytorch.org/get-started/locally/ to get the right install command for your hardware.",
}

func packageInstallHints(missingImports []string, commandPath string) []PackageInstallHint {
	hints := make([]PackageInstallHint, 0, len(missingImports))
	for _, imp := range missingImports {
		pipName := packagePipNames[imp]
		if pipName == "" {
			pipName = imp
		}
		manual := packageManualInstall[imp]
		note := packageNotes[imp]
		command := ""
		if !manual {
			command = commandPath + " -m pip install " + pipName
		}
		hints = append(hints, PackageInstallHint{
			ImportName: imp,
			PipName:    pipName,
			Command:    command,
			Manual:     manual,
			Note:       note,
		})
	}
	return hints
}

// InstallPackagesRequest is the request body for the install endpoint.
type InstallPackagesRequest struct {
	Packages []string `json:"packages"`
}

// InstallPackagesResult is returned after attempting to install packages.
type InstallPackagesResult struct {
	Success      bool     `json:"success"`
	Output       string   `json:"output,omitempty"`
	Observations []string `json:"observations"`
}

// autoInstallPipNames lists packages that are safe to auto-install (torch excluded — too large and GPU-specific).
// lapx is used instead of lap because it ships pre-compiled wheels for ARM (Raspberry Pi, Jetson)
// as well as x86/amd64, avoiding the C toolchain requirement that lap needs on ARM.
var autoInstallPipNames = map[string]string{
	"cv2":         "opencv-python",
	"ultralytics": "ultralytics",
}

// InstallPythonPackages runs pip install for requested packages, restricted to the auto-install allow-list.
func InstallPythonPackages(ctx context.Context, settings VisionToolSettings, packages []string) InstallPackagesResult {
	tool := externaltools.CheckExecutable(ctx, externaltools.ExecutableSpec{
		Name:           "python",
		ConfiguredPath: settings.Command,
		ExecutableName: settings.Command,
		Timeout:        visionToolTimeout(settings.TimeoutMs),
		MaxOutputBytes: 2048,
	})
	if !tool.Found {
		return InstallPackagesResult{Observations: []string{"Python executable not found: " + tool.Error}}
	}

	var pipArgs []string
	var skipped []string
	for _, pkg := range packages {
		pipName, ok := autoInstallPipNames[pkg]
		if !ok {
			skipped = append(skipped, pkg)
			continue
		}
		pipArgs = append(pipArgs, pipName)
	}

	result := InstallPackagesResult{}
	for _, s := range skipped {
		result.Observations = append(result.Observations, "Skipped "+s+": not in auto-install allow-list (install manually).")
	}
	if len(pipArgs) == 0 {
		result.Observations = append(result.Observations, "No packages to install.")
		return result
	}

	args := append([]string{"-m", "pip", "install"}, pipArgs...)
	output, err := externaltools.Probe(ctx, tool.Path, args, 5*time.Minute, 128*1024)
	result.Output = output
	if err != nil {
		result.Observations = append(result.Observations, "Install failed: "+err.Error())
		return result
	}
	result.Success = true
	result.Observations = append(result.Observations, "Installed: "+strings.Join(pipArgs, ", "))
	return result
}
