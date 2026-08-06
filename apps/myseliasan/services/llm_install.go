package services

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/externaltools"
)

// LLMInstaller acquires the sidecar's two artifacts — the llama-server release
// archive and a GGUF model — by either of two routes:
//
//   - Download (internet installs): fetches the PINNED release/model and refuses
//     anything whose SHA-256 does not match the constants in llm_catalog.go.
//     Gated twice: the agent.allowDownloads config flag, and the
//     MYSELIASAN_AI_DOWNLOADS=off environment hard lock (air-gap posture, same
//     contract as the basemap downloader).
//   - Import (air-gapped installs): the operator points the server-side file
//     picker at an archive/model they carried in. Imports can be ANY build or
//     model, so they are not checked against the pins — instead the computed
//     SHA-256 is written to the install log and the audit trail, so what was
//     imported is on the record.
//
// Downloads land as "<dest>.part" and are renamed into place only after the
// checksum passes, so a torn download can never be mistaken for an artifact.
type LLMInstaller struct {
	llmDir  string
	sidecar *LLMSidecar
	// allowDownloads resolves the CONFIG gate live; the env hard lock is layered
	// on top in DownloadsAllowed.
	allowDownloads func() bool
	client         *http.Client
	logf           func(format string, args ...any)
	// onResult is an optional metrics hook: (artifact, method, ok).
	onResult func(artifact, method string, ok bool)

	mu     sync.Mutex
	states map[string]*llmInstallState // keyed "binary" | "model"
}

// LLMInstallState is one artifact's install progress, polled by the UI.
type LLMInstallState struct {
	Running bool   `json:"running"`
	Status  string `json:"status"` // "", "running", "done", "failed"
	Log     string `json:"log"`
	Path    string `json:"path"`
}

type llmInstallState struct {
	running bool
	status  string
	log     []string
	path    string
}

// llmDownloadsEnvKey hard-locks downloads off regardless of config when set to
// off/0/false — for sites whose policy is "this box must never egress".
const llmDownloadsEnvKey = "MYSELIASAN_AI_DOWNLOADS"

const (
	llmArtifactBinary = "binary"
	llmArtifactModel  = "model"
)

// NewLLMInstaller wires the installer over the sidecar (paused around file
// replacement) and the live config gate.
func NewLLMInstaller(llmDir string, sidecar *LLMSidecar, allowDownloads func() bool, logf func(string, ...any)) *LLMInstaller {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &LLMInstaller{
		llmDir:         llmDir,
		sidecar:        sidecar,
		allowDownloads: allowDownloads,
		client:         &http.Client{Timeout: 30 * time.Minute},
		logf:           logf,
		states: map[string]*llmInstallState{
			llmArtifactBinary: {},
			llmArtifactModel:  {},
		},
	}
}

// SetOnResult wires a metrics hook. Optional.
func (i *LLMInstaller) SetOnResult(fn func(artifact, method string, ok bool)) { i.onResult = fn }

// DownloadsAllowed reports whether the download route is available: the env
// hard lock wins, then the config flag.
func (i *LLMInstaller) DownloadsAllowed() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(llmDownloadsEnvKey))) {
	case "off", "0", "false", "no":
		return false
	}
	return i.allowDownloads == nil || i.allowDownloads()
}

// DownloadSupported reports whether this OS/arch has a pinned release asset.
func (i *LLMInstaller) DownloadSupported() bool {
	_, _, _, err := llamaServerDownload(runtime.GOOS, runtime.GOARCH)
	return err == nil
}

// Status snapshots both artifacts' install state for the UI (polled ~1.5s).
func (i *LLMInstaller) Status() map[string]LLMInstallState {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make(map[string]LLMInstallState, len(i.states))
	for k, st := range i.states {
		out[k] = LLMInstallState{
			Running: st.running,
			Status:  st.status,
			Log:     strings.Join(st.log, "\n"),
			Path:    st.path,
		}
	}
	return out
}

// StartBinaryDownload begins the pinned llama.cpp release download in the
// background. Errors that prevent STARTING are returned; progress and failures
// after that surface via Status.
func (i *LLMInstaller) StartBinaryDownload(ctx context.Context) error {
	url, sha, asset, err := llamaServerDownload(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	return i.start(llmArtifactBinary, func(st *llmInstallState) error {
		archive, err := i.download(ctx, st, url, asset, sha, maxBinaryArchiveBytes)
		if err != nil {
			return err
		}
		defer os.Remove(archive)
		return i.installBinaryArchive(ctx, st, archive, "download")
	}, "download")
}

// StartModelDownload begins the pinned default-model download in the background.
func (i *LLMInstaller) StartModelDownload(ctx context.Context) error {
	return i.start(llmArtifactModel, func(st *llmInstallState) error {
		dest := filepath.Join(llmDirModels(i.llmDir), defaultModelFile)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		got, err := i.downloadTo(ctx, st, defaultModelURL, dest, defaultModelSHA, maxModelBytes)
		if err != nil {
			return err
		}
		st.path = got
		i.publishModel(st, got)
		return nil
	}, "download")
}

// ImportBinaryArchive installs llama-server from an operator-supplied release
// archive (.zip or .tar.gz), e.g. carried into an air-gapped site. Synchronous —
// local extraction is fast.
func (i *LLMInstaller) ImportBinaryArchive(ctx context.Context, path string) (string, error) {
	if err := i.begin(llmArtifactBinary, "import"); err != nil {
		return "", err
	}
	st := i.states[llmArtifactBinary]
	err := func() error {
		sum, err := fileSHA256(path)
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		i.appendLog(st, fmt.Sprintf("importing %s (sha256 %s)", filepath.Base(path), sum))
		return i.installBinaryArchive(ctx, st, path, "import")
	}()
	return i.finish(llmArtifactBinary, "import", st, err), err
}

// ImportModel installs an operator-supplied .gguf by copying it under
// <llmDir>/models. Synchronous; returns the installed path.
func (i *LLMInstaller) ImportModel(ctx context.Context, path string) (string, error) {
	if err := i.begin(llmArtifactModel, "import"); err != nil {
		return "", err
	}
	st := i.states[llmArtifactModel]
	err := func() error {
		if !strings.HasSuffix(strings.ToLower(path), ".gguf") {
			return errors.New("model import expects a .gguf file")
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.Size() > maxModelBytes {
			return fmt.Errorf("model exceeds the %d GiB import cap", maxModelBytes>>30)
		}
		sum, err := fileSHA256(path)
		if err != nil {
			return fmt.Errorf("read model: %w", err)
		}
		i.appendLog(st, fmt.Sprintf("importing %s (sha256 %s)", filepath.Base(path), sum))

		dest := filepath.Join(llmDirModels(i.llmDir), filepath.Base(path))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if samePath(path, dest) {
			st.path = dest
			i.publishModel(st, dest)
			return nil
		}
		if err := copyFileAtomic(path, dest); err != nil {
			return err
		}
		st.path = dest
		i.publishModel(st, dest)
		return nil
	}()
	return i.finish(llmArtifactModel, "import", st, err), err
}

// installBinaryArchive extracts a release archive into <llmDir>/bin, locates
// llama-server, probes it, and points the sidecar at it. The sidecar is paused
// through the extraction because a running llama-server holds file locks on
// Windows that would make every overwrite fail.
func (i *LLMInstaller) installBinaryArchive(ctx context.Context, st *llmInstallState, archive, method string) error {
	binDir := llmDirBin(i.llmDir)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	if i.sidecar != nil {
		i.sidecar.Pause()
		defer i.sidecar.Resume()
	}
	i.appendLog(st, "extracting "+filepath.Base(archive))
	if err := extractLLMArchive(archive, binDir); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	exe := findFileUnder(binDir, llamaServerExeName())
	if exe == "" {
		return fmt.Errorf("archive did not contain %s", llamaServerExeName())
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(exe, 0o755)
	}
	i.appendLog(st, "verifying "+exe)
	out, err := externaltools.Probe(ctx, exe, []string{"--version"}, 10*time.Second, 4096)
	if err != nil {
		return fmt.Errorf("llama-server probe failed: %w", err)
	}
	i.appendLog(st, firstLine(out))
	st.path = exe
	if i.sidecar != nil {
		i.sidecar.SetPaths(exe, "")
	}
	i.appendLog(st, "installed — method: "+method)
	return nil
}

// --- download plumbing -----------------------------------------------------

// download fetches url into the OS temp dir and returns the verified file path.
func (i *LLMInstaller) download(ctx context.Context, st *llmInstallState, url, name, wantSHA string, cap int64) (string, error) {
	tmp, err := os.MkdirTemp("", "llm-dl-")
	if err != nil {
		return "", err
	}
	dest := filepath.Join(tmp, name)
	got, err := i.downloadTo(ctx, st, url, dest, wantSHA, cap)
	if err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	return got, nil
}

// downloadTo streams url to "<dest>.part", verifies its SHA-256 against wantSHA,
// and renames into place. On any failure the .part file is removed.
func (i *LLMInstaller) downloadTo(ctx context.Context, st *llmInstallState, url, dest, wantSHA string, cap int64) (string, error) {
	if !i.DownloadsAllowed() {
		return "", errors.New("downloads are disabled on this control plane — use the import path")
	}
	i.appendLog(st, "downloading "+url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	// Some CDNs (Hugging Face, GitHub releases) reject Go's default agent.
	req.Header.Set("User-Agent", "MySeliaSan/1.0")
	resp, err := i.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: unexpected status %s", resp.Status)
	}

	part := dest + ".part"
	out, err := os.Create(part)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, hasher), io.LimitReader(resp.Body, cap+1))
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(part)
		return "", fmt.Errorf("download write: %w", err)
	}
	if n > cap {
		os.Remove(part)
		return "", fmt.Errorf("download exceeded the %d MiB cap", cap>>20)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(got, wantSHA) {
		os.Remove(part)
		return "", fmt.Errorf("checksum mismatch: got %s, want %s — refusing the artifact", got, wantSHA)
	}
	i.appendLog(st, fmt.Sprintf("sha256 verified (%s, %d MiB)", got[:12], n>>20))
	if err := os.Rename(part, dest); err != nil {
		os.Remove(part)
		return "", err
	}
	return dest, nil
}

// publishModel points the sidecar at a freshly installed model.
func (i *LLMInstaller) publishModel(st *llmInstallState, path string) {
	if i.sidecar != nil {
		i.sidecar.SetPaths("", path)
	}
	i.appendLog(st, "model installed at "+path)
}

// --- state bookkeeping -----------------------------------------------------

// start begins a background install for one artifact.
func (i *LLMInstaller) start(artifact string, run func(st *llmInstallState) error, method string) error {
	if err := i.begin(artifact, method); err != nil {
		return err
	}
	st := i.states[artifact]
	go func() {
		err := run(st)
		i.finish(artifact, method, st, err)
	}()
	return nil
}

func (i *LLMInstaller) begin(artifact, method string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	st := i.states[artifact]
	if st == nil {
		return fmt.Errorf("unknown artifact %q", artifact)
	}
	if st.running {
		return fmt.Errorf("%s install already running", artifact)
	}
	st.running = true
	st.status = "running"
	st.log = []string{time.Now().Format("15:04:05") + " " + method + " started"}
	return nil
}

func (i *LLMInstaller) finish(artifact, method string, st *llmInstallState, err error) string {
	i.mu.Lock()
	st.running = false
	if err != nil {
		st.status = "failed"
		st.log = append(st.log, "FAILED: "+err.Error())
	} else {
		st.status = "done"
	}
	path := st.path
	i.mu.Unlock()
	if err != nil {
		i.logf("llm install (%s, %s): %v", artifact, method, err)
	}
	if i.onResult != nil {
		i.onResult(artifact, method, err == nil)
	}
	return path
}

func (i *LLMInstaller) appendLog(st *llmInstallState, line string) {
	i.mu.Lock()
	st.log = append(st.log, time.Now().Format("15:04:05")+" "+line)
	i.mu.Unlock()
}

// --- helpers ---------------------------------------------------------------

func llmDirBin(llmDir string) string    { return filepath.Join(llmDir, "bin") }
func llmDirModels(llmDir string) string { return filepath.Join(llmDir, "models") }

// findFileUnder returns the first file named base under root (recursively) —
// the release archives differ in layout (Windows flat, Linux nested under
// llama-<tag>/), so the location is discovered rather than assumed.
func findFileUnder(root, base string) string {
	var found string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return filepath.SkipAll
		}
		if !d.IsDir() && strings.EqualFold(d.Name(), base) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// extractLLMArchive extracts a .zip or .tar.gz into destDir with slip guards;
// tar symlinks are recreated (the Linux release carries ~10 of them).
func extractLLMArchive(archivePath, destDir string) error {
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		return extractLLMZip(archivePath, destDir)
	}
	if strings.HasSuffix(strings.ToLower(archivePath), ".tar.gz") || strings.HasSuffix(strings.ToLower(archivePath), ".tgz") {
		return extractLLMTarGz(archivePath, destDir)
	}
	return fmt.Errorf("unsupported archive type: %s (expected .zip or .tar.gz)", filepath.Base(archivePath))
}

func extractLLMZip(zipPath, destDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	destClean := filepath.Clean(destDir)
	for _, zf := range zr.File {
		target := filepath.Join(destDir, filepath.FromSlash(zf.Name))
		if !strings.HasPrefix(filepath.Clean(target), destClean+string(os.PathSeparator)) && filepath.Clean(target) != destClean {
			return fmt.Errorf("zip entry escapes dest: %s", zf.Name)
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, zf.Mode()|0o200)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractLLMTarGz(archivePath, destDir string) error {
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
			_, err = io.Copy(out, tr) //nolint:gosec // bounded by the verified archive size cap
			out.Close()
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			// The Linux release ships versioned .so symlinks. Recreate them; ignore
			// failures on filesystems that disallow symlinks.
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = os.Remove(target)
			_ = os.Symlink(hdr.Linkname, target)
		}
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFileAtomic(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	part := dest + ".part"
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(part)
		return err
	}
	if err := os.Rename(part, dest); err != nil {
		os.Remove(part)
		return err
	}
	return nil
}

func samePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return false
	}
	if strings.EqualFold(runtime.GOOS, "windows") {
		return strings.EqualFold(aa, bb)
	}
	return aa == bb
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if l := strings.TrimSpace(line); l != "" {
			return l
		}
	}
	return ""
}
