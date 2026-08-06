package services

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLlamaServerDownloadMatrix(t *testing.T) {
	for _, tc := range []struct {
		goos, goarch string
		wantAsset    string
		wantErr      bool
	}{
		{"windows", "amd64", llamaAssetWinAmd64, false},
		{"linux", "amd64", llamaAssetLinuxAmd64, false},
		{"darwin", "arm64", "", true},
		{"linux", "arm64", "", true},
	} {
		url, sha, asset, err := llamaServerDownload(tc.goos, tc.goarch)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s/%s: expected import-only error", tc.goos, tc.goarch)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s/%s: %v", tc.goos, tc.goarch, err)
			continue
		}
		if asset != tc.wantAsset || !strings.HasSuffix(url, "/"+tc.wantAsset) {
			t.Errorf("%s/%s: url/asset mismatch: %s / %s", tc.goos, tc.goarch, url, asset)
		}
		if len(sha) != 64 {
			t.Errorf("%s/%s: sha pin looks wrong: %q", tc.goos, tc.goarch, sha)
		}
	}
}

func newTestInstaller(t *testing.T, allow bool) *LLMInstaller {
	t.Helper()
	return NewLLMInstaller(t.TempDir(), nil, func() bool { return allow }, nil)
}

func TestDownloadToVerifiesChecksumAndCleansUp(t *testing.T) {
	payload := []byte("definitely a model")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	i := newTestInstaller(t, true)
	st := &llmInstallState{}
	dest := filepath.Join(t.TempDir(), "artifact.bin")

	// Wrong pin: refused, no artifact, no .part left behind.
	wrong := strings.Repeat("00", 32)
	if _, err := i.downloadTo(context.Background(), st, srv.URL, dest, wrong, 1<<20); err == nil ||
		!strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("artifact must not exist after a checksum failure")
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatal(".part must be removed after a checksum failure")
	}

	// Right pin: lands atomically at dest.
	sum := sha256.Sum256(payload)
	got, err := i.downloadTo(context.Background(), st, srv.URL, dest, hex.EncodeToString(sum[:]), 1<<20)
	if err != nil {
		t.Fatalf("downloadTo: %v", err)
	}
	if got != dest {
		t.Fatalf("dest = %q", got)
	}
	data, _ := os.ReadFile(dest)
	if string(data) != string(payload) {
		t.Fatal("artifact content mismatch")
	}
}

func TestDownloadToRespectsSizeCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 4096))
	}))
	defer srv.Close()
	i := newTestInstaller(t, true)
	_, err := i.downloadTo(context.Background(), &llmInstallState{}, srv.URL,
		filepath.Join(t.TempDir(), "x"), strings.Repeat("00", 32), 1024)
	if err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("expected size-cap error, got %v", err)
	}
}

func TestDownloadsDisabledByConfigAndEnv(t *testing.T) {
	// Config gate.
	i := newTestInstaller(t, false)
	if i.DownloadsAllowed() {
		t.Fatal("config false must disable downloads")
	}
	if _, err := i.downloadTo(context.Background(), &llmInstallState{}, "http://example.invalid",
		filepath.Join(t.TempDir(), "x"), "00", 10); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
	// Env hard lock beats config true.
	t.Setenv(llmDownloadsEnvKey, "off")
	j := newTestInstaller(t, true)
	if j.DownloadsAllowed() {
		t.Fatal("env lock must override config")
	}
}

func TestImportModelCopiesAndRefusesNonGguf(t *testing.T) {
	i := newTestInstaller(t, true)
	src := filepath.Join(t.TempDir(), "custom-model.gguf")
	if err := os.WriteFile(src, []byte("gguf bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest, err := i.ImportModel(context.Background(), src)
	if err != nil {
		t.Fatalf("ImportModel: %v", err)
	}
	if filepath.Dir(dest) != llmDirModels(i.llmDir) || filepath.Base(dest) != "custom-model.gguf" {
		t.Fatalf("model landed at %q", dest)
	}
	if data, _ := os.ReadFile(dest); string(data) != "gguf bytes" {
		t.Fatal("copied content mismatch")
	}
	// The import log records the sha256 for the audit trail.
	if st := i.Status()[llmArtifactModel]; !strings.Contains(st.Log, "sha256") {
		t.Fatalf("import log missing sha256: %q", st.Log)
	}
	if _, err := i.ImportModel(context.Background(), filepath.Join(t.TempDir(), "not-a-model.bin")); err == nil {
		t.Fatal("non-.gguf import must be refused")
	}
}

// buildTestArchive makes a zip shaped like the Windows release (flat files, a
// llama-server.exe stub) — Windows can't exec a text stub, so extraction and
// discovery are tested here and the probe path is covered by the live bench.
func TestExtractAndFindServer(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "release.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range map[string]string{
		"ggml.dll":                     "dll",
		"nested/" + "llama-server.exe": "MZ stub",
	} {
		w, _ := zw.Create(name)
		w.Write([]byte(content))
	}
	zw.Close()
	f.Close()

	binDir := filepath.Join(dir, "bin")
	if err := extractLLMArchive(archive, binDir); err != nil {
		t.Fatalf("extract: %v", err)
	}
	found := findFileUnder(binDir, "llama-server.exe")
	if found == "" || !strings.HasSuffix(found, "llama-server.exe") {
		t.Fatalf("llama-server not found after extract, got %q", found)
	}
}

func TestExtractRefusesZipSlip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.zip")
	f, _ := os.Create(archive)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("../../evil.txt")
	w.Write([]byte("nope"))
	zw.Close()
	f.Close()
	if err := extractLLMArchive(archive, filepath.Join(dir, "bin")); err == nil {
		t.Fatal("zip-slip entry must be refused")
	}
}
