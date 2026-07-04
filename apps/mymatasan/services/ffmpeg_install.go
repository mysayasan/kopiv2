package services

import (
	"archive/zip"
	"context"
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

	"github.com/mysayasan/kopiv2/infra/externaltools"
	"github.com/mysayasan/kopiv2/infra/procutil"
)

// FFmpegStatusResult reports whether a usable ffmpeg is available to the app.
type FFmpegStatusResult struct {
	Found   bool   `json:"found"`
	Path    string `json:"path"`
	Version string `json:"version"`
}

// FFmpegInstallState is the live status of a background ffmpeg download/install, polled
// by the setup wizard (same shape as the GPU dependency installer).
type FFmpegInstallState struct {
	Running bool   `json:"running"`
	Status  string `json:"status"` // "", running, done, failed
	Log     string `json:"log"`
	Path    string `json:"path"`
	// Supported is false when no automatic download is available for this OS/arch, so
	// the UI can fall back to a manual path entry instead of offering the button.
	Supported bool `json:"supported"`
}

// FFmpegInstaller downloads a static ffmpeg build for the host OS/arch, installs it
// under binDir, and persists the resolved path into the runtime decoder settings. One
// install runs at a time; the UI polls InstallStatus.
type FFmpegInstaller struct {
	binDir   string
	settings IRuntimeSettingsService
	client   *http.Client

	mu      sync.Mutex
	running bool
	status  string
	logTxt  string
	path    string
}

// NewFFmpegInstaller builds the installer. binDir is where the ffmpeg binary is placed
// (created on demand). settings is used to persist decoder.mjpeg.ffmpegPath on success.
func NewFFmpegInstaller(binDir string, settings IRuntimeSettingsService) *FFmpegInstaller {
	return &FFmpegInstaller{
		binDir:   binDir,
		settings: settings,
		client:   &http.Client{Timeout: 15 * time.Minute},
	}
}

// FFmpegStatus resolves ffmpeg from the configured decoder path, PATH, and the install
// dir, then probes its version.
func (f *FFmpegInstaller) FFmpegStatus(ctx context.Context) FFmpegStatusResult {
	configured := ""
	if f.settings != nil {
		if dec, err := f.settings.Decoder(ctx); err == nil {
			configured = dec.MJPEG.FFmpegPath
		}
	}
	st := externaltools.CheckExecutable(ctx, externaltools.ExecutableSpec{
		Name:           "ffmpeg",
		ConfiguredPath: configured,
		ExecutableName: "ffmpeg",
		CandidatePaths: []string{filepath.Join(f.binDir, ffmpegExeName())},
		ProbeArgs:      []string{"-version"},
		Timeout:        5 * time.Second,
		MaxOutputBytes: 4096,
	})
	res := FFmpegStatusResult{Found: st.Found, Path: st.Path}
	if st.Found {
		res.Version = firstNonEmptyLine(st.ProbeOutput)
	}
	return res
}

// InstallStatus returns the current install state for polling.
func (f *FFmpegInstaller) InstallStatus() FFmpegInstallState {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, _, err := ffmpegDownloadURL(runtime.GOOS, runtime.GOARCH)
	return FFmpegInstallState{
		Running:   f.running,
		Status:    f.status,
		Log:       f.logTxt,
		Path:      f.path,
		Supported: err == nil,
	}
}

// StartInstall begins a background download+install of ffmpeg for this host. It errors
// immediately when auto-download is unsupported for the OS/arch or one is already
// running. The UI polls InstallStatus to follow progress.
func (f *FFmpegInstaller) StartInstall(_ context.Context) error {
	url, kind, err := ffmpegDownloadURL(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	f.mu.Lock()
	if f.running {
		f.mu.Unlock()
		return errors.New("an ffmpeg install is already in progress")
	}
	f.running = true
	f.status = "running"
	f.logTxt = "Starting ffmpeg download…\n"
	f.path = ""
	f.mu.Unlock()

	go f.run(url, kind)
	return nil
}

func (f *FFmpegInstaller) run(url, kind string) {
	defer func() {
		f.mu.Lock()
		f.running = false
		f.mu.Unlock()
	}()

	if err := os.MkdirAll(f.binDir, 0o755); err != nil {
		f.fail(fmt.Sprintf("create install dir: %v", err))
		return
	}
	tmpDir, err := os.MkdirTemp("", "ffmpeg-dl-")
	if err != nil {
		f.fail(fmt.Sprintf("create temp dir: %v", err))
		return
	}
	defer os.RemoveAll(tmpDir)

	ext := ".zip"
	if kind == "tarxz" {
		ext = ".tar.xz"
	}
	archive := filepath.Join(tmpDir, "ffmpeg"+ext)
	f.appendLog(fmt.Sprintf("Downloading %s\n", url))
	if err := f.download(url, archive); err != nil {
		f.fail(fmt.Sprintf("download failed: %v", err))
		return
	}

	f.appendLog("Extracting…\n")
	var binPath string
	switch kind {
	case "zip":
		binPath, err = extractFFmpegFromZip(archive, f.binDir)
	case "tarxz":
		binPath, err = extractFFmpegFromTarXz(archive, tmpDir, f.binDir)
	default:
		err = fmt.Errorf("unknown archive kind %q", kind)
	}
	if err != nil {
		f.fail(fmt.Sprintf("extract failed: %v", err))
		return
	}

	f.appendLog("Verifying…\n")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := externaltools.Probe(ctx, binPath, []string{"-version"}, 8*time.Second, 4096)
	if err != nil {
		f.fail(fmt.Sprintf("the downloaded ffmpeg did not run: %v", err))
		return
	}
	f.appendLog(firstNonEmptyLine(out) + "\n")

	if f.settings != nil {
		if err := f.persistPath(binPath); err != nil {
			f.fail(fmt.Sprintf("ffmpeg installed but saving the path failed: %v", err))
			return
		}
	}

	f.mu.Lock()
	f.status = "done"
	f.path = binPath
	f.logTxt += "Done. ffmpeg installed at " + binPath + "\nRestart the app to use it everywhere.\n"
	f.mu.Unlock()
}

// persistPath writes the installed ffmpeg path into the runtime decoder settings so all
// ffmpeg-using subsystems (recording, live stream, capture) resolve it after restart.
func (f *FFmpegInstaller) persistPath(binPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	settings, err := f.settings.Get(ctx)
	if err != nil {
		return err
	}
	settings.Decoder.MJPEG.FFmpegPath = binPath
	_, err = f.settings.Save(ctx, settings)
	return err
}

func (f *FFmpegInstaller) download(url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := f.client.Do(req)
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

func (f *FFmpegInstaller) appendLog(line string) {
	f.mu.Lock()
	f.logTxt += line
	f.mu.Unlock()
}

func (f *FFmpegInstaller) fail(msg string) {
	f.mu.Lock()
	f.status = "failed"
	f.logTxt += "[failed] " + msg + "\n"
	f.mu.Unlock()
}

// ffmpegDownloadURL returns the static-build download URL and archive kind ("zip" or
// "tarxz") for the given OS/arch, or an error when no automatic download is available.
// Pure (no I/O) so the OS/arch matrix is unit-testable.
func ffmpegDownloadURL(goos, goarch string) (url string, kind string, err error) {
	switch goos {
	case "windows":
		switch goarch {
		case "amd64":
			return "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip", "zip", nil
		case "arm64":
			return "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-winarm64-gpl.zip", "zip", nil
		}
	case "linux":
		switch goarch {
		case "amd64":
			return "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz", "tarxz", nil
		case "arm64":
			return "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-arm64-static.tar.xz", "tarxz", nil
		}
	case "darwin":
		// evermeet ships x86_64 builds; on Apple Silicon they run under Rosetta 2.
		if goarch == "amd64" || goarch == "arm64" {
			return "https://evermeet.cx/ffmpeg/getrelease/ffmpeg/zip", "zip", nil
		}
	}
	return "", "", fmt.Errorf("automatic ffmpeg download is not available for %s/%s — install ffmpeg manually and set its path", goos, goarch)
}

// extractFFmpegFromZip writes the ffmpeg (and ffprobe, if present) binary from a zip
// archive into destDir, matching by base name regardless of the archive's folder
// layout. Returns the installed ffmpeg path.
func extractFFmpegFromZip(zipPath, destDir string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	ffmpegPath := ""
	for _, entry := range zr.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		base := strings.ToLower(filepath.Base(entry.Name))
		if !isFFmpegBinaryName(base) {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return "", err
		}
		dst := filepath.Join(destDir, filepath.Base(entry.Name))
		if err := writeBinary(dst, rc); err != nil {
			rc.Close()
			return "", err
		}
		rc.Close()
		if strings.HasPrefix(base, "ffmpeg") {
			ffmpegPath = dst
		}
	}
	if ffmpegPath == "" {
		return "", errors.New("no ffmpeg binary found in the downloaded archive")
	}
	return ffmpegPath, nil
}

// extractFFmpegFromTarXz extracts a .tar.xz via the system tar (xz support is built in
// on Linux/macOS) into tmpDir, then copies the ffmpeg/ffprobe binaries into destDir.
func extractFFmpegFromTarXz(archivePath, tmpDir, destDir string) (string, error) {
	extractDir := filepath.Join(tmpDir, "x")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// -xf autodetects xz compression on both GNU tar and bsdtar.
	cmd := exec.CommandContext(ctx, "tar", "-xf", archivePath, "-C", extractDir)
	procutil.HideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("tar: %v: %s", err, strings.TrimSpace(string(out)))
	}
	ffmpegPath := ""
	err := filepath.Walk(extractDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		base := strings.ToLower(info.Name())
		if !isFFmpegBinaryName(base) {
			return nil
		}
		dst := filepath.Join(destDir, info.Name())
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := writeBinary(dst, in); err != nil {
			return err
		}
		if strings.HasPrefix(base, "ffmpeg") {
			ffmpegPath = dst
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if ffmpegPath == "" {
		return "", errors.New("no ffmpeg binary found in the downloaded archive")
	}
	return ffmpegPath, nil
}

// isFFmpegBinaryName matches the ffmpeg/ffprobe executable base names across platforms.
func isFFmpegBinaryName(base string) bool {
	switch base {
	case "ffmpeg", "ffmpeg.exe", "ffprobe", "ffprobe.exe":
		return true
	}
	return false
}

// writeBinary writes r to dst (replacing it) with an executable mode.
func writeBinary(dst string, r io.Reader) error {
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func ffmpegExeName() string {
	if runtime.GOOS == "windows" {
		return "ffmpeg.exe"
	}
	return "ffmpeg"
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
