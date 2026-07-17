package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mysayasan/kopiv2/infra/procutil"
)

// pythonFaceEmbedder is the production FaceEmbedder: it runs faces_worker.py (YuNet + SFace via cv2)
// once per enrollment image and parses the faces it found. It is the enrollment counterpart to the
// LIVE face stage inside the persistent detector worker — both use the same model files and the
// shared face_model.py, so an enrolled faceprint and a live faceprint are directly comparable.
//
// Enrollment is occasional and admin-driven, so a one-shot process (load model, embed, exit) is fine
// and avoids contending with the live detector — the same tradeoff anomaly_worker.py makes for
// fitting a bank.
type pythonFaceEmbedder struct {
	python     string // interpreter (e.g. "python")
	script     string // absolute path to faces_worker.py
	yunetPath  string // YuNet detector .onnx
	sfacePath  string // SFace recognizer .onnx
	logf       func(string, ...any)
	timeout    time.Duration
}

// NewPythonFaceEmbedder builds the production embedder. If the model files or script are missing the
// embedder still constructs; Embed then returns a clear error (so a fresh install without the face
// models fails at enrollment with a helpful message rather than at startup).
func NewPythonFaceEmbedder(python, script, yunetPath, sfacePath string, logf func(string, ...any)) FaceEmbedder {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &pythonFaceEmbedder{
		python: defaultStr(python, "python"), script: script,
		yunetPath: yunetPath, sfacePath: sfacePath, logf: logf, timeout: 60 * time.Second,
	}
}

func (e *pythonFaceEmbedder) Model() string { return "opencv-sface-128" }

type faceWorkerResult struct {
	Faces []faceWorkerFace `json:"faces"`
	Error string           `json:"error"`
}

type faceWorkerFace struct {
	Vector  []float32  `json:"vector"`
	Box     [4]float64 `json:"box"`
	Quality float64    `json:"quality"`
	Thumb   string     `json:"thumb"`
}

func (e *pythonFaceEmbedder) Embed(ctx context.Context, imageJPEG []byte) ([]DetectedFace, error) {
	if strings.TrimSpace(e.script) == "" {
		return nil, fmt.Errorf("face recognition is not set up on this host (worker script missing)")
	}
	if !faceFileExists(e.yunetPath) || !faceFileExists(e.sfacePath) {
		return nil, fmt.Errorf("face models are not installed — run the face-recognition setup to download them")
	}

	tmp, err := os.CreateTemp("", "faceenroll-*.jpg")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(imageJPEG); err != nil {
		tmp.Close()
		return nil, err
	}
	tmp.Close()

	cctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, e.python, e.script, "--embed", tmpPath, "--yunet", e.yunetPath, "--sface", e.sfacePath)
	procutil.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("face worker failed: %w", err)
	}

	var res faceWorkerResult
	if err := json.Unmarshal(trimToJSON(out), &res); err != nil {
		return nil, fmt.Errorf("face worker returned unreadable output: %w", err)
	}
	if res.Error != "" {
		return nil, fmt.Errorf("face worker: %s", res.Error)
	}

	faces := make([]DetectedFace, 0, len(res.Faces))
	for _, f := range res.Faces {
		var thumb []byte
		if f.Thumb != "" {
			thumb, _ = base64.StdEncoding.DecodeString(f.Thumb)
		}
		faces = append(faces, DetectedFace{Vector: f.Vector, Box: f.Box, Quality: f.Quality, ThumbJPEG: thumb})
	}
	return faces, nil
}

func faceFileExists(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// trimToJSON returns the substring from the first '{' to the last '}', so any stray stderr/torch
// noise that leaked onto stdout before the JSON line does not break the parse.
func trimToJSON(b []byte) []byte {
	start := -1
	end := -1
	for i, c := range b {
		if c == '{' && start < 0 {
			start = i
		}
		if c == '}' {
			end = i
		}
	}
	if start >= 0 && end > start {
		return b[start : end+1]
	}
	return b
}

// facesWorkerScript resolves faces_worker.py next to the given detector script (mirrors the anomaly
// worker resolution), so it is found in both the dev tree (ai/) and the staged tree (bin/ai/).
func facesWorkerScript(detectorScript string) string {
	if strings.TrimSpace(detectorScript) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(detectorScript), "faces_worker.py")
}
