package logging

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileLoggerWritesAndListsNewestFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.log")
	logger, err := NewFileLogger(Config{
		Enabled: true,
		Path:    path,
	})
	if err != nil {
		t.Fatalf("expected logger, got error: %v", err)
	}
	defer logger.Close()

	logger.Infof("service", "first")
	logger.Warnf("service", "second")

	entries, total, err := logger.List(context.Background(), 1, 0)
	if err != nil {
		t.Fatalf("expected list entries, got error: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one limited entry, got %d", len(entries))
	}
	if entries[0].Message != "second" || entries[0].Level != "warn" || entries[0].OS == "" {
		t.Fatalf("unexpected newest entry: %#v", entries[0])
	}
}

func TestDisabledLoggerListsEmpty(t *testing.T) {
	logger, err := NewFileLogger(Config{Enabled: false})
	if err != nil {
		t.Fatalf("expected disabled logger, got error: %v", err)
	}
	defer logger.Close()

	logger.Infof("service", "not persisted")
	entries, total, err := logger.List(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("expected no list error, got: %v", err)
	}
	if total != 0 || len(entries) != 0 {
		t.Fatalf("expected empty disabled logger list, got total=%d entries=%#v", total, entries)
	}
}

func TestFileLoggerDeleteByMonthRemovesMatchingDatedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime.log")
	logger, err := NewFileLogger(Config{
		Enabled: true,
		Path:    path,
	})
	if err != nil {
		t.Fatalf("expected logger, got error: %v", err)
	}
	defer logger.Close()

	juneFile := filepath.Join(dir, "runtime-2025-06-01.log")
	julyFile := filepath.Join(dir, "runtime-2025-07-01.log")
	if err := os.WriteFile(juneFile, []byte(`{"timestamp":1748736000,"time":"2025-06-01T00:00:00Z","level":"info","source":"test","message":"june","os":"test"}`+"\n"), 0640); err != nil {
		t.Fatalf("write june log: %v", err)
	}
	if err := os.WriteFile(julyFile, []byte(`{"timestamp":1751328000,"time":"2025-07-01T00:00:00Z","level":"info","source":"test","message":"july","os":"test"}`+"\n"), 0640); err != nil {
		t.Fatalf("write july log: %v", err)
	}

	deleted, err := logger.DeleteByMonth(context.Background(), 2025, 6)
	if err != nil {
		t.Fatalf("delete by month failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected one deleted file, got %d", deleted)
	}
	if _, err := os.Stat(juneFile); !os.IsNotExist(err) {
		t.Fatalf("expected june file to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(julyFile); err != nil {
		t.Fatalf("expected july file to remain, stat err=%v", err)
	}

	entries, total, err := logger.List(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("list after delete failed: %v", err)
	}
	if total != 1 || len(entries) != 1 || entries[0].Message != "july" {
		t.Fatalf("expected only july entry after delete, total=%d entries=%#v", total, entries)
	}
}

func TestFileLoggerDeleteOlderThanRemovesOnlyOldDatedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime.log")
	logger, err := NewFileLogger(Config{
		Enabled: true,
		Path:    path,
	})
	if err != nil {
		t.Fatalf("expected logger, got error: %v", err)
	}
	defer logger.Close()

	oldFile := filepath.Join(dir, "runtime-2025-01-01.log")
	newFile := filepath.Join(dir, "runtime-2025-03-01.log")
	if err := os.WriteFile(oldFile, []byte(`{"timestamp":1735689600,"time":"2025-01-01T00:00:00Z","level":"info","source":"test","message":"old","os":"test"}`+"\n"), 0640); err != nil {
		t.Fatalf("write old log: %v", err)
	}
	if err := os.WriteFile(newFile, []byte(`{"timestamp":1740787200,"time":"2025-03-01T00:00:00Z","level":"info","source":"test","message":"new","os":"test"}`+"\n"), 0640); err != nil {
		t.Fatalf("write new log: %v", err)
	}

	cutoff := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	deleted, err := logger.DeleteOlderThan(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("delete older than failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected one deleted file, got %d", deleted)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("expected old file to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatalf("expected new file to remain, stat err=%v", err)
	}
}
