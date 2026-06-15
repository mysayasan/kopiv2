package notification

import (
	"context"
	"testing"
)

func TestPurgeRejectsNonPositiveCutoff(t *testing.T) {
	s := &Service{}
	if _, err := s.Purge(context.Background(), 0, false); err == nil {
		t.Fatal("expected error for olderThan=0")
	}
	if _, err := s.Purge(context.Background(), -1, true); err == nil {
		t.Fatal("expected error for negative olderThan")
	}
}

func TestPurgeOlderThanDaysRejectsNonPositiveDays(t *testing.T) {
	s := &Service{}
	if _, err := s.PurgeOlderThanDays(context.Background(), 0, false); err == nil {
		t.Fatal("expected error for days=0")
	}
}
