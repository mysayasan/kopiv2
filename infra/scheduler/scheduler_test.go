package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSchedulerStartPeriodicRunsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := New(ctx, nil)
	calls := int32(0)
	done := make(chan struct{})

	s.StartPeriodic("test", time.Hour, func(context.Context) error {
		if atomic.AddInt32(&calls, 1) == 1 {
			close(done)
		}
		return nil
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected task to run immediately")
	}

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected one immediate call, got %d", calls)
	}
}
