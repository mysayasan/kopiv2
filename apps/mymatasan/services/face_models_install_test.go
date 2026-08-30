package services

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheckFaceModelFile covers the two ways a model "download" succeeds and is still not a model.
// Both produce an HTTP 200 and a file on disk, so nothing downstream would notice until opencv
// refuses the file with a much less obvious error.
func TestCheckFaceModelFile(t *testing.T) {
	dir := t.TempDir()

	write := func(name string, body []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}

	// A git-LFS pointer: what GitHub serves for an LFS-tracked file through the wrong URL form.
	// It is small, so the size check would also catch it — but the message must name the real
	// cause, because "too small" sends somebody looking at their network instead of the URL.
	pointer := []byte("version https://git-lfs.github.com/spec/v1\noid sha256:deadbeef\nsize 38713798\n")
	p := write("pointer.onnx", pointer)
	if err := checkFaceModelFile(p, int64(len(pointer)), faceSfaceMinBytes); err == nil {
		t.Fatal("an LFS pointer must be rejected")
	} else if got := err.Error(); got != "the server returned a git-LFS pointer instead of the model file" {
		t.Errorf("wrong reason for an LFS pointer: %q", got)
	}

	// A truncated transfer: right shape, wrong length.
	short := make([]byte, 1024)
	p = write("short.onnx", short)
	if err := checkFaceModelFile(p, int64(len(short)), faceSfaceMinBytes); err == nil {
		t.Fatal("a truncated download must be rejected")
	}

	// A plausible model passes: bigger than the floor and not a pointer.
	big := make([]byte, faceYunetMinBytes+10)
	big[0] = 0x08 // onnx protobuf-ish first byte; the check does not parse, it just must not reject
	p = write("ok.onnx", big)
	if err := checkFaceModelFile(p, int64(len(big)), faceYunetMinBytes); err != nil {
		t.Errorf("a full-size file must pass: %v", err)
	}
}

// TestFaceModelsStatusReportsWhatIsMissing proves the status is per-FILE rather than all-or-nothing.
// The common real state on a host that has used the export face-blur is "detector present,
// recognizer missing", and a status that could not say that would offer to re-download both.
func TestFaceModelsStatusReportsWhatIsMissing(t *testing.T) {
	dir := t.TempDir()
	yunet := filepath.Join(dir, "face_detection_yunet_2023mar.onnx")
	sface := filepath.Join(dir, "face_recognition_sface_2021dec.onnx")
	worker := filepath.Join(dir, "faces_worker.py")
	if err := os.WriteFile(yunet, []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(worker, []byte("#"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A python that cannot be run: the probe must report that rather than claiming readiness.
	inst := NewFaceModelsInstaller(yunet, sface, worker, func() string { return filepath.Join(dir, "no-such-python") })
	st := inst.Status(t.Context())

	if st.Ready {
		t.Error("must not be ready with the recognizer missing")
	}
	if st.MissingModels != 1 {
		t.Errorf("MissingModels = %d, want 1", st.MissingModels)
	}
	if !st.Worker {
		t.Error("the worker script is present and must be reported so")
	}
	if len(st.Models) != 2 || !st.Models[0].Present || st.Models[1].Present {
		t.Errorf("per-file presence wrong: %+v", st.Models)
	}
	if st.Models[0].Role != "detector" || st.Models[1].Role != "recognizer" {
		t.Errorf("roles wrong: %+v", st.Models)
	}
	if st.Python.Found {
		t.Error("a python that cannot be run must not be reported as found")
	}
	if !st.NeedsOpenCV {
		t.Error("no usable opencv must be reported as needing it")
	}
}

// TestStartInstallRefusesUnwritableDir proves the directory is checked BEFORE the download rather
// than at the final rename — 37 MB is a long way to travel to learn the destination is read-only.
func TestStartInstallRefusesUnwritableDir(t *testing.T) {
	dir := t.TempDir()
	// A path whose PARENT is a file cannot be created as a directory on any OS, which is the
	// portable way to make MkdirAll fail (chmod 0500 does not stop the owner on Windows).
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(blocker, "sub", "face_recognition_sface_2021dec.onnx")
	inst := NewFaceModelsInstaller(bad, bad, bad, func() string { return "python" })
	if err := inst.StartInstall(t.Context()); err == nil {
		t.Fatal("an unwritable model directory must be refused up front")
	}
	if st := inst.InstallStatus(); st.Running {
		t.Error("a refused install must not leave the job marked running")
	}
}
