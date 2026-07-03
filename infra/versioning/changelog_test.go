package versioning

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderChangelogEntryGroupsByTypeWithVersions(t *testing.T) {
	manifest := Manifest{
		Core: Entry{Version: "1.42.0"},
		Apps: map[string]Entry{"mymatasan": {Version: "1.74.0"}},
	}
	changes := []Change{
		{Type: "added", App: "mymatasan", Summary: "Add talk-back mic button"},
		{Type: "fixed", App: "mymatasan", Summary: "PTZ D-pad continuous move"},
		{Type: "changed", Scope: "core", Summary: "Home/data dir split"},
	}
	entry := RenderChangelogEntry(changes, manifest, true, []string{"mymatasan"}, "475575e5cd", time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC))

	for _, want := range []string{
		"## 2026-07-02 — mymatasan 1.74.0, core 1.42.0 (475575e)",
		"### Added",
		"- **mymatasan**: Add talk-back mic button",
		"### Changed",
		"- **core**: Home/data dir split",
		"### Fixed",
		"- **mymatasan**: PTZ D-pad continuous move",
	} {
		if !strings.Contains(entry, want) {
			t.Errorf("entry missing %q\n---\n%s", want, entry)
		}
	}
	// Group order: Added before Changed before Fixed.
	if a, c := strings.Index(entry, "### Added"), strings.Index(entry, "### Changed"); a > c {
		t.Errorf("Added should come before Changed")
	}
}

func TestPrependChangelogEntryCreatesAndOrdersNewestFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CHANGELOG.md")

	if err := PrependChangelogEntry(path, "## 2026-07-01 — mymatasan 1.73.0\n\n### Added\n- **mymatasan**: older\n"); err != nil {
		t.Fatal(err)
	}
	if err := PrependChangelogEntry(path, "## 2026-07-02 — mymatasan 1.74.0\n\n### Added\n- **mymatasan**: newer\n"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.HasPrefix(out, "# Changelog") {
		t.Errorf("expected changelog header, got:\n%s", out)
	}
	newer := strings.Index(out, "1.74.0")
	older := strings.Index(out, "1.73.0")
	if newer < 0 || older < 0 || newer > older {
		t.Errorf("newest entry should appear first (newer=%d older=%d)\n%s", newer, older, out)
	}
}

func TestApplyPendingChangesWritesChangelog(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "version.json")
	pendingDir := filepath.Join(root, "changes", "pending")
	appliedDir := filepath.Join(root, "changes", "applied")
	changelogPath := filepath.Join(root, "CHANGELOG.md")
	changeDir := filepath.Join(pendingDir, "20260702-000000-sample")

	if err := os.MkdirAll(changeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"core":{"version":"1.0.0"},"apps":{"mymatasan":{"version":"1.0.0"}},"commit":"","updatedAt":""}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(changeDir, "change.json"), []byte(`{"type":"added","scope":"app","app":"mymatasan","summary":"Installer packaging"}`), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := ApplyPendingChanges(ApplyOptions{
		ManifestPath:  manifestPath,
		PendingDir:    pendingDir,
		AppliedDir:    appliedDir,
		Commit:        "deadbeefcafe",
		ChangelogPath: changelogPath,
		Now:           time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ApplyPendingChanges: %v", err)
	}
	if len(res.AppliedChanges) != 1 {
		t.Fatalf("expected 1 applied change, got %d", len(res.AppliedChanges))
	}

	data, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("changelog not written: %v", err)
	}
	out := string(data)
	for _, want := range []string{"mymatasan 1.1.0", "### Added", "Installer packaging", "(deadbee)"} {
		if !strings.Contains(out, want) {
			t.Errorf("changelog missing %q\n---\n%s", want, out)
		}
	}
}
