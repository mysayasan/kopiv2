package recording

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShredFileRemoves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(path, []byte("sensitive footage payload"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ShredFile(path, 3); err != nil {
		t.Fatalf("ShredFile: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file should be gone, stat err = %v", err)
	}
	// No leftover .shred temp files in the directory.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("expected empty dir, got %d entries", len(entries))
	}
}

func TestShredFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.ts")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := ShredFile(path, 1); err != nil {
		t.Fatalf("ShredFile on empty file: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty file should be gone, stat err = %v", err)
	}
}

func TestSecureRemoveDeadlinePastStillDeletes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.mp4")
	if err := os.WriteFile(path, []byte("sensitive footage payload"), 0644); err != nil {
		t.Fatal(err)
	}
	// A deadline already in the past skips the overwrite but must still unlink the file.
	if err := SecureRemoveDeadline(path, 3, time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("SecureRemoveDeadline: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file should be gone, stat err = %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("expected empty dir, got %d entries", len(entries))
	}
}

func TestSecureRemoveMissingIsOK(t *testing.T) {
	if err := SecureRemove(filepath.Join(t.TempDir(), "nope.mp4"), 3); err != nil {
		t.Fatalf("missing file should be OK, got %v", err)
	}
	if err := SecureRemove("", 3); err != nil {
		t.Fatalf("empty path should be OK, got %v", err)
	}
}

func TestSecureRemovePlainWhenZeroPasses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.mp4")
	if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SecureRemove(path, 0); err != nil {
		t.Fatalf("SecureRemove(0): %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file should be gone, stat err = %v", err)
	}
}
