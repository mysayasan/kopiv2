package versioning

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyPendingChangesBumpsAndMovesEntries(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "version.json")
	pendingDir := filepath.Join(root, "changes", "pending")
	appliedDir := filepath.Join(root, "changes", "applied")
	changeDir := filepath.Join(pendingDir, "20260607-120000-sample")

	if err := os.MkdirAll(changeDir, 0755); err != nil {
		t.Fatalf("mkdir change dir: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"core":{"version":"1.0.0"},"apps":{"mymatasan":{"version":"1.0.0"}},"commit":"","updatedAt":""}`), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(changeDir, "change.json"), []byte(`{"level":"minor","scope":"both","app":"mymatasan","summary":"sample"}`), 0644); err != nil {
		t.Fatalf("write change: %v", err)
	}

	res, err := ApplyPendingChanges(ApplyOptions{
		ManifestPath: manifestPath,
		PendingDir:   pendingDir,
		AppliedDir:   appliedDir,
		Commit:       "abc123",
		Now:          time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ApplyPendingChanges failed: %v", err)
	}
	if !res.Changed {
		t.Fatalf("expected changed result")
	}
	if res.Manifest.Core.Version != "1.1.0" {
		t.Fatalf("core version got %s", res.Manifest.Core.Version)
	}
	if res.Manifest.Apps["mymatasan"].Version != "1.1.0" {
		t.Fatalf("app version got %s", res.Manifest.Apps["mymatasan"].Version)
	}
	if _, err := os.Stat(filepath.Join(appliedDir, "20260607-120000-sample", "change.json")); err != nil {
		t.Fatalf("expected applied change file: %v", err)
	}
}
