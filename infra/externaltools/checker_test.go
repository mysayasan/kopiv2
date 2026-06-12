package externaltools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveExecutableUsesConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.exe")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	resolved, source, err := ResolveExecutable(path, "missing-tool", nil)
	if err != nil {
		t.Fatalf("ResolveExecutable() error = %v", err)
	}
	if source != "configured" {
		t.Fatalf("source = %q, want configured", source)
	}
	if resolved != path {
		t.Fatalf("resolved = %q, want %q", resolved, path)
	}
}

func TestResolveExecutableUsesCandidatePathWhenPathLookupMisses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "candidate.exe")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	resolved, source, err := ResolveExecutable("", "definitely-missing-tool", []string{path})
	if err != nil {
		t.Fatalf("ResolveExecutable() error = %v", err)
	}
	if source != "candidate" {
		t.Fatalf("source = %q, want candidate", source)
	}
	if resolved != path {
		t.Fatalf("resolved = %q, want %q", resolved, path)
	}
}

func TestResolveExecutableFallsBackFromConfiguredNameToCandidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "candidate.exe")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	resolved, source, err := ResolveExecutable("definitely-missing-tool", "", []string{path})
	if err != nil {
		t.Fatalf("ResolveExecutable() error = %v", err)
	}
	if source != "candidate" {
		t.Fatalf("source = %q, want candidate", source)
	}
	if resolved != path {
		t.Fatalf("resolved = %q, want %q", resolved, path)
	}
}

func TestCheckExecutableReportsMissingTool(t *testing.T) {
	status := CheckExecutable(context.Background(), ExecutableSpec{
		Name:           "Missing",
		ExecutableName: "definitely-missing-tool",
		Timeout:        10 * time.Millisecond,
	})

	if status.Found {
		t.Fatalf("Found = true, want false")
	}
	if !strings.Contains(status.Error, "definitely-missing-tool executable not found") {
		t.Fatalf("Error = %q", status.Error)
	}
}
