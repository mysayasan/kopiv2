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

func TestApplyPendingChangesSupportsTypeAndCommaScopes(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "version.json")
	pendingDir := filepath.Join(root, "changes", "pending")
	appliedDir := filepath.Join(root, "changes", "applied")

	if err := os.WriteFile(manifestPath, []byte(`{"core":{"version":"1.0.0"},"apps":{"myidsan":{"version":"1.0.0"},"mymatasan":{"version":"1.0.0"}},"commit":"","updatedAt":""}`), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	changes := map[string]string{
		"20260607-153000-sso-rbac-upgrade":                 `{"type":"minor","scope":"core,myidsan,mymatasan","summary":"sso"}`,
		"20260607-171500-move-identity-modules-to-myidsan": `{"type":"changed","scope":"myidsan,shared,apphost","summary":"move identity"}`,
		"20260607-173000-myidsan-tls-config-docs":          `{"type":"fixed","scope":"myidsan,docs,cleanup","summary":"tls docs"}`,
	}
	for name, body := range changes {
		changeDir := filepath.Join(pendingDir, name)
		if err := os.MkdirAll(changeDir, 0755); err != nil {
			t.Fatalf("mkdir change dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(changeDir, "change.json"), []byte(body), 0644); err != nil {
			t.Fatalf("write change: %v", err)
		}
	}

	res, err := ApplyPendingChanges(ApplyOptions{
		ManifestPath: manifestPath,
		PendingDir:   pendingDir,
		AppliedDir:   appliedDir,
		Commit:       "def456",
		Now:          time.Date(2026, 6, 7, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ApplyPendingChanges failed: %v", err)
	}
	if !res.Changed {
		t.Fatalf("expected changed result")
	}
	if res.Manifest.Core.Version != "1.2.0" {
		t.Fatalf("core version got %s", res.Manifest.Core.Version)
	}
	if res.Manifest.Apps["myidsan"].Version != "1.2.1" {
		t.Fatalf("myidsan version got %s", res.Manifest.Apps["myidsan"].Version)
	}
	if res.Manifest.Apps["mymatasan"].Version != "1.1.0" {
		t.Fatalf("mymatasan version got %s", res.Manifest.Apps["mymatasan"].Version)
	}
}
