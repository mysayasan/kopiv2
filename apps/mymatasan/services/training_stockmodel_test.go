package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The setup wizard's model picker sends the bare variant name ("yolo11x") while
// Settings sends the listed file name ("yolo11x.pt"). Both must resolve to a
// download; a miss here falls through to the local-path branch and the user is
// told "model file not found: yolo11x" with no way to get the model at all.
func TestCanonicalStockModelAcceptsBareNames(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"yolo11x", "yolo11x.pt", true},
		{"yolo11x.pt", "yolo11x.pt", true},
		{"YOLO11L", "yolo11l.pt", true},
		{" yolo11n ", "yolo11n.pt", true},
		{"", "", false},
		{"yolo11", "", false},
		{"/models/custom.pt", "", false},
	} {
		got, ok := canonicalStockModel(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("canonicalStockModel(%q) = %q,%v; want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSetStockModelBareNameTakesDownloadPath(t *testing.T) {
	dir := t.TempDir()
	s := &trainingService{
		dataDir:        dir,
		stockModelFile: filepath.Join(dir, "stock_model.txt"),
	}
	// PythonCmd is empty, so the download itself cannot run — what matters is
	// which branch we land in: the download's complaint, not "file not found".
	err := s.SetStockModel(context.Background(), "yolo11x", 0)
	if err == nil {
		t.Fatal("expected an error with no python configured")
	}
	if strings.Contains(err.Error(), "model file not found") {
		t.Fatalf("bare name was treated as a local path: %v", err)
	}
	if !strings.Contains(err.Error(), "python") {
		t.Fatalf("expected the download path to complain about python, got: %v", err)
	}
}

func TestSetStockModelBareDefaultReverts(t *testing.T) {
	dir := t.TempDir()
	ptr := filepath.Join(dir, "stock_model.txt")
	if err := os.WriteFile(ptr, []byte(filepath.Join(dir, "yolo11x.pt")), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &trainingService{dataDir: dir, stockModelFile: ptr}
	if err := s.SetStockModel(context.Background(), "yolo11n", 0); err != nil {
		t.Fatalf("reverting to the bundled default failed: %v", err)
	}
	if _, err := os.Stat(ptr); !os.IsNotExist(err) {
		t.Fatalf("expected the pointer file to be removed, stat err = %v", err)
	}
}

func TestSetStockModelUnknownPathStillRejected(t *testing.T) {
	dir := t.TempDir()
	s := &trainingService{dataDir: dir, stockModelFile: filepath.Join(dir, "stock_model.txt")}
	err := s.SetStockModel(context.Background(), filepath.Join(dir, "nope.pt"), 0)
	if err == nil || !strings.Contains(err.Error(), "model file not found") {
		t.Fatalf("expected a not-found error for a missing local path, got: %v", err)
	}
}
