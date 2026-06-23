package services

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// FsEntry is one item in a directory listing for the file picker.
type FsEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
}

// FsBrowseResult is a single directory level: its absolute path, the parent to
// navigate up to (empty at an allowed root), the OS path separator, and its entries.
type FsBrowseResult struct {
	Path      string    `json:"path"`
	Parent    string    `json:"parent"`
	Separator string    `json:"separator"`
	Entries   []FsEntry `json:"entries"`
}

// BrowseDirectory lists the immediate entries of dir for the server-side file
// picker (e.g. choosing the ffmpeg binary). Browsing is confined to a whitelist of
// allowed roots (the app directory and its bin/, the user's home, and common
// install locations) — an empty path, or anything outside those roots, lists the
// roots themselves rather than exposing the whole filesystem. It is read-only:
// names only, never file contents. The API layer additionally gates this to admins.
// extraRoots are additional site-specific roots from config (decoder.browseRoots).
func BrowseDirectory(dir string, extraRoots []string) (FsBrowseResult, error) {
	sep := string(os.PathSeparator)
	roots := allowedBrowseRoots(extraRoots)
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return rootListing(roots, sep), nil
	}

	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil {
		// A non-existent or non-absolute seed (e.g. the default "ffmpeg" resolved
		// from PATH) isn't an error — just start the picker at the allowed roots.
		return rootListing(roots, sep), nil
	}
	if !info.IsDir() {
		dir = filepath.Dir(dir) // a file was passed — browse its folder
	}
	// Enforce the whitelist: anything outside the allowed roots falls back to the
	// roots listing rather than exposing arbitrary directories.
	if _, ok := withinAllowed(dir, roots); !ok {
		return rootListing(roots, sep), nil
	}

	items, err := os.ReadDir(dir)
	if err != nil {
		return FsBrowseResult{}, fmt.Errorf("cannot read %s: %w", dir, err)
	}

	entries := make([]FsEntry, 0, len(items))
	for _, it := range items {
		name := it.Name()
		if strings.HasPrefix(name, ".") {
			continue // hide dotfiles; they can still be typed manually
		}
		entries = append(entries, FsEntry{
			Name: name,
			Path: filepath.Join(dir, name),
			Dir:  it.IsDir(),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir // directories first
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	return FsBrowseResult{
		Path:      dir,
		Parent:    allowedParent(dir, roots),
		Separator: sep,
		Entries:   entries,
	}, nil
}

// rootListing presents the whitelisted roots as the top-level picker entries.
func rootListing(roots []string, sep string) FsBrowseResult {
	entries := make([]FsEntry, 0, len(roots))
	for _, r := range roots {
		entries = append(entries, FsEntry{Name: r, Path: r, Dir: true})
	}
	return FsBrowseResult{Path: "", Parent: "", Separator: sep, Entries: entries}
}

// allowedParent returns the directory to navigate up to. At an allowed root (or
// when the parent would escape the whitelist) it returns "" so the picker shows the
// roots listing instead of leaving the sandbox.
func allowedParent(dir string, roots []string) string {
	for _, r := range roots {
		if pathEqual(dir, r) {
			return ""
		}
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		return ""
	}
	if _, ok := withinAllowed(parent, roots); ok {
		return parent
	}
	return ""
}

// allowedBrowseRoots builds the set of directories the picker may browse: the app
// working directory and its bin/ (where the in-app installer places ffmpeg), the
// user's home, and OS-specific common locations. Only existing paths are returned,
// de-duplicated. extra holds site-specific roots from config (decoder.browseRoots).
func allowedBrowseRoots(extra []string) []string {
	var roots []string
	add := func(p string) {
		if strings.TrimSpace(p) == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return
		}
		abs = filepath.Clean(abs)
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return
		}
		for _, existing := range roots {
			if pathEqual(existing, abs) {
				return
			}
		}
		roots = append(roots, abs)
	}

	if wd, err := os.Getwd(); err == nil {
		add(wd)
		add(filepath.Join(wd, "bin"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(home)
	}
	switch runtime.GOOS {
	case "windows":
		add(`C:\ffmpeg`)
		add(`C:\ffmpeg\bin`)
		add(os.Getenv("ProgramFiles"))
		add(os.Getenv("ProgramFiles(x86)"))
		add(os.Getenv("ProgramData"))
		add(os.Getenv("LOCALAPPDATA"))
	case "darwin":
		add("/usr/bin")
		add("/usr/local/bin") // Intel Homebrew
		add("/opt/homebrew")  // Apple Silicon Homebrew
		add("/opt/local")     // MacPorts
		add("/Applications")
	default: // linux and other unix
		add("/usr/bin")
		add("/usr/local/bin")
		add("/opt")
		add("/snap/bin")
		add("/bin")
	}
	// Site-specific roots from config (decoder.browseRoots).
	for _, p := range extra {
		add(p)
	}
	return roots
}

// withinAllowed reports whether dir is one of, or nested under, an allowed root.
func withinAllowed(dir string, roots []string) (string, bool) {
	nd := normForCompare(dir)
	for _, r := range roots {
		nr := normForCompare(r)
		if nd == nr || strings.HasPrefix(nd, nr+string(os.PathSeparator)) {
			return r, true
		}
	}
	return "", false
}

// normForCompare normalizes a path for comparison — case-insensitively on Windows.
func normForCompare(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

func pathEqual(a, b string) bool {
	return normForCompare(a) == normForCompare(b)
}
