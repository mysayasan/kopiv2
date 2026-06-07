package services

import (
	"context"
	"errors"
	"testing"
	"time"

	applog "github.com/mysayasan/kopiv2/infra/logging"
)

type fakeRuntimeLogger struct {
	deleteByMonthCalls int
	deleteOlderCalls   int
	cutoff             time.Time
}

func (f *fakeRuntimeLogger) Write(p []byte) (int, error)   { return len(p), nil }
func (f *fakeRuntimeLogger) Close() error                  { return nil }
func (f *fakeRuntimeLogger) Debugf(string, string, ...any) {}
func (f *fakeRuntimeLogger) Infof(string, string, ...any)  {}
func (f *fakeRuntimeLogger) Warnf(string, string, ...any)  {}
func (f *fakeRuntimeLogger) Errorf(string, string, ...any) {}
func (f *fakeRuntimeLogger) List(context.Context, uint64, uint64) ([]applog.Entry, uint64, error) {
	return nil, 0, nil
}
func (f *fakeRuntimeLogger) DeleteByMonth(context.Context, int, int) (uint64, error) {
	f.deleteByMonthCalls++
	return 1, nil
}
func (f *fakeRuntimeLogger) DeleteOlderThan(_ context.Context, cutoff time.Time) (uint64, error) {
	f.deleteOlderCalls++
	f.cutoff = cutoff
	return 2, nil
}
func (f *fakeRuntimeLogger) Path() string { return "" }

func TestRuntimeLogServiceRejectsCurrentMonthDelete(t *testing.T) {
	logger := &fakeRuntimeLogger{}
	service := NewRuntimeLogService(logger)
	now := time.Now()

	deleted, err := service.DeleteByMonth(context.Background(), now.Year(), int(now.Month()))
	if !errors.Is(err, ErrCurrentMonthLogDelete) {
		t.Fatalf("expected current month delete error, got deleted=%d err=%v", deleted, err)
	}
	if logger.deleteByMonthCalls != 0 {
		t.Fatalf("expected service guard to block logger call, got %d calls", logger.deleteByMonthCalls)
	}
}

func TestRuntimeLogServiceRetentionDelegatesCutoff(t *testing.T) {
	logger := &fakeRuntimeLogger{}
	service := NewRuntimeLogService(logger)

	deleted, err := service.DeleteOlderThan(context.Background(), 30)
	if err != nil {
		t.Fatalf("expected retention delete, got error: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected deleted count from logger, got %d", deleted)
	}
	if logger.deleteOlderCalls != 1 {
		t.Fatalf("expected one delete older call, got %d", logger.deleteOlderCalls)
	}
	if logger.cutoff.IsZero() {
		t.Fatalf("expected cutoff to be set")
	}
}
