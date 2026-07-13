package services

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/versioning"
)

const updateRepo = "mysayasan/kopiv2"

// updateRestarter is the subset of apphost.Restarter the updater needs (avoids an
// apphost import from services).
type updateRestarter interface {
	Restart(reason string)
}

// UpdateInfo is the self-update status surfaced to the UI.
type UpdateInfo struct {
	Current         string `json:"current"`
	Latest          string `json:"latest,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	CanSelfUpdate   bool   `json:"canSelfUpdate"`
	Managed         string `json:"managed,omitempty"` // "", "package", "docker"
	HTMLURL         string `json:"htmlUrl,omitempty"`
	PublishedAt     string `json:"publishedAt,omitempty"`
	CheckedAt       int64  `json:"checkedAt,omitempty"`
	Error           string `json:"error,omitempty"`
	// Apply job state (empty until an update is applied).
	Applying    bool   `json:"applying"`
	ApplyStatus string `json:"applyStatus,omitempty"` // running, done, failed
	ApplyLog    string `json:"applyLog,omitempty"`
}

// UpdateService checks GitHub Releases for a newer mymatasan and, on portable /
// installer installs, downloads + verifies + swaps the binary and web/AI assets in
// place then restarts. A scheduled check keeps the "update available" state fresh.
type UpdateService struct {
	current   string
	homeDir   string
	restarter updateRestarter
	client    *http.Client

	mu          sync.Mutex
	latest      string
	htmlURL     string
	publishedAt string
	checkedAt   int64
	checkErr    string
	applying    bool
	applyStatus string
	applyLog    string
}

// NewUpdateService builds the updater. currentVersion is the running app version;
// homeDir is the read-only app root whose binary + static/ + ai/ get swapped.
func NewUpdateService(currentVersion, homeDir string, restarter updateRestarter) *UpdateService {
	return &UpdateService{
		current:   strings.TrimPrefix(strings.TrimSpace(currentVersion), "v"),
		homeDir:   homeDir,
		restarter: restarter,
		client:    &http.Client{Timeout: 15 * time.Minute},
	}
}

// RegisterScheduler starts the periodic update check (runs immediately, then every
// interval) so the UI's "update available" state stays timely without polling.
func (u *UpdateService) RegisterScheduler(start func(name string, interval time.Duration, run func(context.Context) error)) {
	if start == nil {
		return
	}
	start("update-check", 6*time.Hour, func(ctx context.Context) error {
		u.CheckNow(ctx)
		return nil
	})
}

// managedKind reports how the install is managed ("package" for deb/rpm, "docker",
// or "" for portable/installer), from the MYMATASAN_MANAGED env the packages set.
func managedKind() string {
	return strings.ToLower(strings.TrimSpace(os.Getenv("MYMATASAN_MANAGED")))
}

// canSelfUpdate is true only when the app owns its files (home dir writable) and the
// install isn't managed by a package manager / container image.
func (u *UpdateService) canSelfUpdate() bool {
	if managedKind() != "" {
		return false
	}
	probe := filepath.Join(u.homeDir, ".mmupdate-writable")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		return false
	}
	os.Remove(probe)
	return true
}

// Status returns the cached check result + any in-flight apply state.
func (u *UpdateService) Status() UpdateInfo {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.snapshot()
}

func (u *UpdateService) snapshot() UpdateInfo {
	info := UpdateInfo{
		Current:       u.current,
		Latest:        u.latest,
		Managed:       managedKind(),
		CanSelfUpdate: u.canSelfUpdate(),
		HTMLURL:       u.htmlURL,
		PublishedAt:   u.publishedAt,
		CheckedAt:     u.checkedAt,
		Error:         u.checkErr,
		Applying:      u.applying,
		ApplyStatus:   u.applyStatus,
		ApplyLog:      u.applyLog,
	}
	info.UpdateAvailable = u.latest != "" && versionGreater(u.latest, u.current)
	return info
}

// CheckNow queries the GitHub latest release and caches the result.
func (u *UpdateService) CheckNow(ctx context.Context) UpdateInfo {
	tag, htmlURL, publishedAt, err := u.fetchLatest(ctx)
	u.mu.Lock()
	u.checkedAt = time.Now().Unix()
	if err != nil {
		u.checkErr = err.Error()
	} else {
		u.checkErr = ""
		u.latest = strings.TrimPrefix(tag, "v")
		u.htmlURL = htmlURL
		u.publishedAt = publishedAt
	}
	info := u.snapshot()
	u.mu.Unlock()
	return info
}

func (u *UpdateService) fetchLatest(ctx context.Context) (tag, htmlURL, publishedAt string, err error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+updateRepo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mymatasan-updater")
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	var rel struct {
		TagName     string `json:"tag_name"`
		HTMLURL     string `json:"html_url"`
		PublishedAt string `json:"published_at"`
		Assets      []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", "", err
	}
	return rel.TagName, rel.HTMLURL, rel.PublishedAt, nil
}

// StartUpdate begins the download + verify + swap + restart in the background. It
// errors immediately when self-update isn't allowed here or one is already running.
func (u *UpdateService) StartUpdate(ctx context.Context) error {
	if !u.canSelfUpdate() {
		return errors.New("self-update is not available for this install type")
	}
	u.mu.Lock()
	if u.applying {
		u.mu.Unlock()
		return errors.New("an update is already in progress")
	}
	if u.latest == "" || !versionGreater(u.latest, u.current) {
		u.mu.Unlock()
		return errors.New("no newer version is available")
	}
	u.applying = true
	u.applyStatus = "running"
	u.applyLog = "Starting update to " + u.latest + "…\n"
	u.mu.Unlock()

	go u.run()
	return nil
}

func (u *UpdateService) run() {
	defer func() {
		u.mu.Lock()
		u.applying = false
		u.mu.Unlock()
	}()

	ctx := context.Background()
	tag, _, _, err := u.fetchLatest(ctx)
	if err != nil {
		u.fail("re-check release: " + err.Error())
		return
	}
	assetURL, checksumsURL, assetName, err := u.selectAssets(ctx, tag)
	if err != nil {
		u.fail(err.Error())
		return
	}

	staging := filepath.Join(u.homeDir, ".mmupdate")
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		u.fail("staging dir: " + err.Error())
		return
	}
	defer os.RemoveAll(staging)

	archivePath := filepath.Join(staging, assetName)
	u.log("Downloading " + assetName + "…\n")
	if err := u.download(assetURL, archivePath); err != nil {
		u.fail("download: " + err.Error())
		return
	}

	u.log("Verifying checksum…\n")
	if err := u.verifyChecksum(ctx, checksumsURL, assetName, archivePath); err != nil {
		u.fail("checksum: " + err.Error())
		return
	}

	u.log("Extracting…\n")
	extractDir := filepath.Join(staging, "extract")
	if err := extractArchive(archivePath, extractDir); err != nil {
		u.fail("extract: " + err.Error())
		return
	}

	u.log("Applying…\n")
	if err := u.swap(extractDir); err != nil {
		u.fail("apply: " + err.Error())
		return
	}

	u.mu.Lock()
	u.applyStatus = "done"
	u.applyLog += "Updated to " + u.latest + ". Restarting…\n"
	u.mu.Unlock()

	// Restart into the freshly-swapped binary.
	if u.restarter != nil {
		go func() {
			time.Sleep(800 * time.Millisecond)
			u.restarter.Restart("self-update")
		}()
	}
}

// selectAssets picks the archive matching this OS/arch plus the checksums file.
func (u *UpdateService) selectAssets(ctx context.Context, tag string) (assetURL, checksumsURL, assetName string, err error) {
	_ = tag
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+updateRepo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mymatasan-updater")
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	var rel struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", "", err
	}
	// The repo ships more than one product (myseliasan releases from the same repo, and
	// its archives carry the identical _<os>_<arch> suffix and extension). Matching on
	// the suffix alone would let this updater download myseliasan and overwrite mymatasan
	// with it — the checksum would even verify, since both land in one checksums.txt.
	// Require the product prefix so an asset can only ever be OUR archive.
	prefix := "mymatasan_"
	suffix := "_" + runtime.GOOS + "_" + runtime.GOARCH
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	for _, a := range rel.Assets {
		if strings.HasPrefix(a.Name, prefix) && strings.Contains(a.Name, suffix) && strings.HasSuffix(a.Name, ext) {
			assetURL, assetName = a.URL, a.Name
		}
		if strings.EqualFold(a.Name, "checksums.txt") {
			checksumsURL = a.URL
		}
	}
	if assetURL == "" {
		return "", "", "", fmt.Errorf("no release archive for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if checksumsURL == "" {
		return "", "", "", errors.New("release has no checksums.txt")
	}
	return assetURL, checksumsURL, assetName, nil
}

func (u *UpdateService) verifyChecksum(ctx context.Context, checksumsURL, assetName, archivePath string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, checksumsURL, nil)
	resp, err := u.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	want := ""
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("no checksum listed for %s", assetName)
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("checksum mismatch (got %s, want %s)", got, want)
	}
	return nil
}

// swap replaces the running binary and the static/ + ai/ dirs from the extracted
// release. Directory swaps stay within homeDir (same filesystem) so renames are
// atomic; the running exe is renamed aside on Windows (can't overwrite in place).
func (u *UpdateService) swap(extractDir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	newExe := filepath.Join(extractDir, filepath.Base(exe))
	if _, err := os.Stat(newExe); err != nil {
		// Archive binary is named "mymatasan(.exe)".
		alt := "mymatasan"
		if runtime.GOOS == "windows" {
			alt = "mymatasan.exe"
		}
		newExe = filepath.Join(extractDir, alt)
		if _, err := os.Stat(newExe); err != nil {
			return errors.New("binary missing from release archive")
		}
	}

	// Binary: copy into the home dir first (same FS), then rename into place.
	pending := exe + ".new"
	if err := copyFile(newExe, pending); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(exe + ".old")
		if err := os.Rename(exe, exe+".old"); err != nil {
			return fmt.Errorf("rename running exe: %w", err)
		}
	}
	if err := os.Rename(pending, exe); err != nil {
		return fmt.Errorf("install new exe: %w", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(exe, 0o755)
	}

	// static/ and ai/ (present in the archive next to the binary).
	for _, dir := range []string{"static", "ai"} {
		src := filepath.Join(extractDir, dir)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(u.homeDir, dir)
		old := dst + ".old"
		_ = os.RemoveAll(old)
		if _, err := os.Stat(dst); err == nil {
			if err := os.Rename(dst, old); err != nil {
				return fmt.Errorf("swap %s: %w", dir, err)
			}
		}
		if err := os.Rename(src, dst); err != nil {
			_ = os.Rename(old, dst) // best-effort rollback
			return fmt.Errorf("install %s: %w", dir, err)
		}
	}
	return nil
}

// CleanupStaleFiles removes leftovers from a previous update (the renamed-aside exe
// and dirs). Call once at startup.
func (u *UpdateService) CleanupStaleFiles() {
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
		_ = os.Remove(exe + ".new")
	}
	_ = os.RemoveAll(filepath.Join(u.homeDir, ".mmupdate"))
	_ = os.RemoveAll(filepath.Join(u.homeDir, "static.old"))
	_ = os.RemoveAll(filepath.Join(u.homeDir, "ai.old"))
}

func (u *UpdateService) download(url, dest string) error {
	resp, err := u.client.Get(url)
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

func (u *UpdateService) log(line string) {
	u.mu.Lock()
	u.applyLog += line
	u.mu.Unlock()
}

func (u *UpdateService) fail(msg string) {
	u.mu.Lock()
	u.applyStatus = "failed"
	u.applyLog += "[failed] " + msg + "\n"
	u.mu.Unlock()
}

// extractArchive extracts a .tar.gz or .zip into destDir.
func extractArchive(archivePath, destDir string) error {
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		return extractZipAll(archivePath, destDir)
	}
	return extractTarGz(archivePath, destDir)
}

func extractZipAll(zipPath, destDir string) error {
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
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}
	return nil
}

// versionGreater reports whether semver a > b (non-semver inputs compare false).
func versionGreater(a, b string) bool {
	av, err1 := versioning.ParseSemVer(strings.TrimPrefix(a, "v"))
	bv, err2 := versioning.ParseSemVer(strings.TrimPrefix(b, "v"))
	if err1 != nil || err2 != nil {
		return false
	}
	if av.Major != bv.Major {
		return av.Major > bv.Major
	}
	if av.Minor != bv.Minor {
		return av.Minor > bv.Minor
	}
	return av.Patch > bv.Patch
}
