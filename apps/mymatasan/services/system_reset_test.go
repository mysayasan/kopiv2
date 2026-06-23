package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemResetDisabledByDefault(t *testing.T) {
	s := NewSystemResetService(SystemResetConfig{}) // AllowReset defaults to false
	if s.Allowed() {
		t.Fatal("reset should be disabled without bootstrap.allowReset")
	}
	if err := s.Start(); err == nil {
		t.Fatal("Start() should error when reset is disabled")
	}
}

func TestSystemResetEraseMediaWipesDirs(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "cam1", "live")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "a.mp4"), []byte("footage payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.ts"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewSystemResetService(SystemResetConfig{
		ShredPasses:       1,
		CollectMediaPaths: func(ctx context.Context) []string { return []string{dir} },
	})
	roots := s.collectRoots(context.Background())
	s.eraseMedia(roots)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("media root should be removed, stat err = %v", err)
	}
	if p := s.Progress(); p.Percent < 45 {
		t.Fatalf("progress should advance through erase, got %d%%", p.Percent)
	}
}

func TestSystemResetNeverWipesWorkingDir(t *testing.T) {
	// A "." or empty path must be skipped so a reset can't nuke the working dir.
	s := NewSystemResetService(SystemResetConfig{
		CollectMediaPaths: func(ctx context.Context) []string { return []string{".", "", "  "} },
	})
	roots := s.collectRoots(context.Background())
	if len(roots) != 0 {
		t.Fatalf("working-dir / empty paths must be skipped, got roots %v", roots)
	}
	s.eraseMedia(roots) // no-op
	if _, err := os.Stat("."); err != nil {
		t.Fatalf("working directory must survive a reset, stat err = %v", err)
	}
}
