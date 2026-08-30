package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/procutil"
)

// Face recognition needs two model files and one Python package, none of which ship with the app:
// the models are large binaries with their own licences, and opencv-python is a wheel. Until this
// installer existed the only way to get them was to run ai/setup.ps1 -Faces from a shell — so the
// enrolment screen's honest "face models are not installed" was a dead end for anyone who was not
// going to open PowerShell. AN ERROR WITHOUT A ROUTE TO THE FIX IS A HALF-BUILT FEATURE. This is the
// same shape as the ffmpeg and AI-runtime installers: a status probe, a background job with a live
// log, and a button in the UI.
//
// The two models are permissively licensed and come from OpenCV's own model zoo:
//   - YuNet  (MIT, ~230 KB)  face DETECTION — also used by the export face-blur, so it is often
//     already present when SFace is not.
//   - SFace  (Apache-2.0, ~37 MB) face RECOGNITION — turns a face into the faceprint that is
//     compared. Enrolment needs BOTH.
const (
	faceYunetURL = "https://github.com/opencv/opencv_zoo/raw/main/models/face_detection_yunet/face_detection_yunet_2023mar.onnx"
	faceSfaceURL = "https://github.com/opencv/opencv_zoo/raw/main/models/face_recognition_sface/face_recognition_sface_2021dec.onnx"

	// Floor sizes for the sanity check below. They are deliberately far under the real sizes
	// (~230 KB and ~37 MB): the point is to catch a truncated download or a git-LFS pointer, not
	// to pin a version.
	faceYunetMinBytes = 100 * 1024
	faceSfaceMinBytes = 20 * 1024 * 1024
)

// FaceModelFile is one required model file and whether it is on disk.
type FaceModelFile struct {
	// Role is "detector" (YuNet) or "recognizer" (SFace) — what the file is FOR, which is what a
	// message about it has to say. "face_recognition_sface_2021dec.onnx is missing" means nothing
	// to the person who has to decide whether to click Install.
	Role    string `json:"role"`
	File    string `json:"file"`
	Present bool   `json:"present"`
	Bytes   int64  `json:"bytes"`
	SizeMB  int    `json:"sizeMb"` // approximate download size, for the button's copy
}

// FacePythonStatus reports whether the interpreter the detector runs on can actually do face work.
// A host can have both models and still fail, because the models are loaded BY opencv-python.
type FacePythonStatus struct {
	Found  bool   `json:"found"`
	Python string `json:"python"`
	OpenCV string `json:"opencv,omitempty"`
	// API is true when this opencv exposes FaceDetectorYN + FaceRecognizerSF (4.5.4+). An old
	// opencv imports fine and then cannot do faces at all, which is a different repair.
	API   bool   `json:"api"`
	Error string `json:"error,omitempty"`
}

// FaceModelsStatus is everything the UI needs to say why enrolment cannot run and what to do.
type FaceModelsStatus struct {
	Ready  bool             `json:"ready"`
	Dir    string           `json:"dir"`
	Worker bool             `json:"worker"`
	Models []FaceModelFile  `json:"models"`
	Python FacePythonStatus `json:"python"`
	// Missing summarises what Install would fetch, so the UI does not have to re-derive it.
	MissingModels int  `json:"missingModels"`
	NeedsOpenCV   bool `json:"needsOpenCV"`
}

// FaceModelsInstallState is the live status of the background install, polled by the UI (same
// shape as the ffmpeg and AI-runtime installers).
type FaceModelsInstallState struct {
	Running bool   `json:"running"`
	Status  string `json:"status"` // "", running, done, failed
	Log     string `json:"log"`
}

// FaceModelsInstaller probes and installs the face-recognition prerequisites: pip-installs
// opencv-python when the detector's interpreter lacks it, then downloads the missing .onnx models
// into the directory the worker reads them from.
type FaceModelsInstaller struct {
	yunetPath  string
	sfacePath  string
	workerPath string
	// python is resolved lazily through the callback so a runtime settings change (or the AI
	// runtime installer repointing vision.detector.command) is picked up without a restart.
	python func() string
	client *http.Client

	mu      sync.Mutex
	running bool
	status  string
	logTxt  string
}

// NewFaceModelsInstaller builds the installer from the resolved model paths (see
// app/wire_vision.go's detectorModelPaths) and a resolver for the detector's interpreter.
func NewFaceModelsInstaller(yunetPath, sfacePath, workerPath string, python func() string) *FaceModelsInstaller {
	if python == nil {
		python = func() string { return "python" }
	}
	return &FaceModelsInstaller{
		yunetPath: yunetPath, sfacePath: sfacePath, workerPath: workerPath, python: python,
		client: &http.Client{Timeout: 20 * time.Minute},
	}
}

// faceProbeScript prints one JSON line describing this interpreter's opencv face support.
const faceProbeScript = "import json\n" +
	"out = {'opencv': '', 'api': False}\n" +
	"try:\n" +
	"    import cv2\n" +
	"    out['opencv'] = cv2.__version__\n" +
	"    out['api'] = hasattr(cv2, 'FaceDetectorYN') and hasattr(cv2, 'FaceRecognizerSF')\n" +
	"except Exception as e:\n" +
	"    out['error'] = str(e)\n" +
	"print(json.dumps(out))"

// Status reports what is present and what is missing. It never mutates anything, so the UI can
// call it freely (on dialog open, after an install, behind a Check button).
func (f *FaceModelsInstaller) Status(ctx context.Context) FaceModelsStatus {
	models := []FaceModelFile{
		faceModelFile("detector", f.yunetPath, 1),
		faceModelFile("recognizer", f.sfacePath, 37),
	}
	st := FaceModelsStatus{
		Dir:    filepath.Dir(f.sfacePath),
		Worker: faceFileExists(f.workerPath),
		Models: models,
		Python: f.probePython(ctx),
	}
	for _, m := range models {
		if !m.Present {
			st.MissingModels++
		}
	}
	st.NeedsOpenCV = !st.Python.API
	st.Ready = st.Worker && st.MissingModels == 0 && st.Python.API
	return st
}

func faceModelFile(role, path string, sizeMB int) FaceModelFile {
	m := FaceModelFile{Role: role, File: filepath.Base(path), SizeMB: sizeMB}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		m.Present = true
		m.Bytes = info.Size()
	}
	return m
}

func (f *FaceModelsInstaller) probePython(ctx context.Context) FacePythonStatus {
	exe := strings.TrimSpace(f.python())
	if exe == "" {
		exe = "python"
	}
	out := FacePythonStatus{Python: exe}
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(pctx, exe, "-c", faceProbeScript)
	procutil.HideWindow(cmd)
	raw, err := cmd.Output()
	if err != nil {
		out.Error = fmt.Sprintf("could not run %q: %v", exe, err)
		return out
	}
	out.Found = true
	var parsed struct {
		OpenCV string `json:"opencv"`
		API    bool   `json:"api"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(trimToJSON(raw), &parsed); err != nil {
		out.Error = "could not read the interpreter's reply"
		return out
	}
	out.OpenCV = parsed.OpenCV
	out.API = parsed.API
	out.Error = parsed.Error
	return out
}

// InstallStatus returns the current background-job state for polling.
func (f *FaceModelsInstaller) InstallStatus() FaceModelsInstallState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return FaceModelsInstallState{Running: f.running, Status: f.status, Log: f.logTxt}
}

// StartInstall begins the background install. It refuses when one is already running, and when the
// model directory cannot be written — checked BEFORE anything is downloaded, because a 37 MB
// download that fails at the final rename is a worse way to learn the directory is read-only.
func (f *FaceModelsInstaller) StartInstall(ctx context.Context) error {
	dir := filepath.Dir(f.sfacePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cannot create the model directory %s: %w", dir, err)
	}
	probe := filepath.Join(dir, ".facemodel-write-test")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		return fmt.Errorf("the model directory %s is not writable by the app: %w", dir, err)
	}
	_ = os.Remove(probe)

	f.mu.Lock()
	if f.running {
		f.mu.Unlock()
		return errors.New("a face-model install is already in progress")
	}
	f.running = true
	f.status = "running"
	f.logTxt = "Starting face-recognition setup…\n"
	f.mu.Unlock()

	go f.run()
	return nil
}

func (f *FaceModelsInstaller) run() {
	defer func() {
		f.mu.Lock()
		f.running = false
		f.mu.Unlock()
	}()

	ctx := context.Background()

	// 1. opencv-python, if the detector's interpreter cannot do faces. This is the same package the
	//    LPR path already installs on most hosts, so it is usually a no-op.
	py := f.probePython(ctx)
	if !py.API {
		if !py.Found {
			f.fail(fmt.Sprintf("the detector's Python (%s) could not be run: %s — install the AI runtime first (Settings › AI), then try again", py.Python, py.Error))
			return
		}
		f.appendLog(fmt.Sprintf("\n$ %s -m pip install opencv-python numpy\n", py.Python))
		args := []string{"-m", "pip", "install", "--disable-pip-version-check", "--no-warn-script-location", "--upgrade", "opencv-python>=4.5.4", "numpy>=1.21"}
		if err := f.runStreaming(py.Python, args); err != nil {
			f.fail(fmt.Sprintf("installing opencv-python failed: %v", err))
			return
		}
		if again := f.probePython(ctx); !again.API {
			f.fail("opencv-python installed but this interpreter still has no FaceDetectorYN/FaceRecognizerSF — it may be too old a build")
			return
		}
		f.appendLog("opencv-python is ready.\n")
	} else {
		f.appendLog(fmt.Sprintf("opencv %s already present (%s).\n", py.OpenCV, py.Python))
	}

	// 2. The model files. Only the missing ones — a present YuNet is not re-fetched.
	downloads := []struct {
		path, url string
		min       int64
		label     string
	}{
		{f.yunetPath, faceYunetURL, faceYunetMinBytes, "face detector (YuNet, MIT)"},
		{f.sfacePath, faceSfaceURL, faceSfaceMinBytes, "face recognizer (SFace, Apache-2.0)"},
	}
	for _, d := range downloads {
		if faceFileExists(d.path) {
			f.appendLog(fmt.Sprintf("%s already present.\n", d.label))
			continue
		}
		f.appendLog(fmt.Sprintf("Downloading the %s…\n", d.label))
		if err := f.downloadModel(d.url, d.path, d.min); err != nil {
			f.fail(fmt.Sprintf("downloading the %s failed: %v", d.label, err))
			return
		}
		f.appendLog(fmt.Sprintf("Saved %s.\n", filepath.Base(d.path)))
	}

	// 3. Prove the models LOAD, rather than trusting that a file of the right size is the right
	//    file. This is the same call the worker makes, on the same interpreter.
	f.appendLog("Verifying the models load…\n")
	if err := f.verifyModels(ctx); err != nil {
		f.fail(fmt.Sprintf("the downloaded models did not load: %v", err))
		return
	}

	f.mu.Lock()
	f.status = "done"
	f.logTxt += "Done. Face recognition is ready — no restart needed; you can enrol a photo now.\n"
	f.mu.Unlock()
}

// faceVerifyScript loads both models exactly as the worker does. Printing "ok" is the only success.
const faceVerifyScript = "import sys, cv2\n" +
	"cv2.FaceDetectorYN.create(sys.argv[1], '', (320, 320))\n" +
	"cv2.FaceRecognizerSF.create(sys.argv[2], '')\n" +
	"print('ok')"

func (f *FaceModelsInstaller) verifyModels(ctx context.Context) error {
	vctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(vctx, strings.TrimSpace(f.python()), "-c", faceVerifyScript, f.yunetPath, f.sfacePath)
	procutil.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(lastLine(string(out))))
	}
	if !strings.Contains(string(out), "ok") {
		return fmt.Errorf("unexpected reply: %s", strings.TrimSpace(lastLine(string(out))))
	}
	return nil
}

// downloadModel fetches url to a temp file beside dest, checks it, and only then moves it into
// place — so a failed or interrupted download never leaves a half-file that `faceFileExists` would
// happily report as an installed model.
func (f *FaceModelsInstaller) downloadModel(url, dest string, minBytes int64) error {
	resp, err := f.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned %s", resp.Status)
	}
	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := checkFaceModelFile(tmp, n, minBytes); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

// checkFaceModelFile rejects the two ways this download goes wrong quietly: a git-LFS POINTER
// (GitHub serves a ~130-byte text stub for LFS-tracked files through some URL forms, and it is a
// perfectly valid 200) and a truncated transfer. Either would be written as a .onnx that opencv
// then refuses with a far less obvious error.
func checkFaceModelFile(path string, size, minBytes int64) error {
	head := make([]byte, 64)
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	n, _ := io.ReadFull(file, head)
	file.Close()
	if strings.HasPrefix(string(head[:n]), "version https://git-lfs") {
		return errors.New("the server returned a git-LFS pointer instead of the model file")
	}
	if size < minBytes {
		return fmt.Errorf("the download was only %d bytes, which is too small to be the model", size)
	}
	return nil
}

func (f *FaceModelsInstaller) runStreaming(python string, args []string) error {
	cmd := exec.Command(python, args...)
	procutil.HideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		f.appendLog(scanner.Text() + "\n")
	}
	return cmd.Wait()
}

func (f *FaceModelsInstaller) appendLog(line string) {
	f.mu.Lock()
	f.logTxt += line
	f.mu.Unlock()
}

func (f *FaceModelsInstaller) fail(msg string) {
	f.mu.Lock()
	f.status = "failed"
	f.logTxt += "[failed] " + msg + "\n"
	f.mu.Unlock()
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}
