package coordination

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/infra/telemetry"
)

type testRecorder struct {
	mu      sync.Mutex
	metrics []telemetry.CoordinationMetric
}

func (r *testRecorder) ObserveCoordination(metric telemetry.CoordinationMetric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics = append(r.metrics, metric)
}

func (r *testRecorder) hasOutcome(outcome string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, metric := range r.metrics {
		if metric.Outcome == outcome {
			return true
		}
	}
	return false
}

func TestMemoryLockerWaitsFIFOUntilRelease(t *testing.T) {
	locker := NewMemoryLocker(Config{
		AppName:      "test",
		WaitTimeout:  time.Second,
		StuckTimeout: time.Hour,
		PollInterval: 5 * time.Millisecond,
	}, nil)

	first, err := locker.Lock(context.Background(), "resource")
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}

	acquired := make(chan Lock, 1)
	go func() {
		lock, err := locker.Lock(context.Background(), "resource")
		if err == nil {
			acquired <- lock
		}
	}()

	select {
	case lock := <-acquired:
		_ = lock.Release(context.Background())
		t.Fatal("second lock acquired before first release")
	case <-time.After(30 * time.Millisecond):
	}

	if err := first.Release(context.Background()); err != nil {
		t.Fatalf("first release failed: %v", err)
	}

	select {
	case lock := <-acquired:
		_ = lock.Release(context.Background())
	case <-time.After(time.Second):
		t.Fatal("second lock did not acquire after first release")
	}
}

func TestMemoryLockerRecordsTimeoutAndStuck(t *testing.T) {
	rec := &testRecorder{}
	locker := NewMemoryLocker(Config{
		AppName:      "test",
		WaitTimeout:  20 * time.Millisecond,
		StuckTimeout: 20 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
	}, rec)

	first, err := locker.Lock(context.Background(), "resource")
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}
	defer first.Release(context.Background())

	_, err = locker.Lock(context.Background(), "resource")
	if err == nil {
		t.Fatal("second lock unexpectedly acquired")
	}

	time.Sleep(40 * time.Millisecond)

	if !rec.hasOutcome("timeout") {
		t.Fatalf("timeout metric missing: %+v", rec.metrics)
	}
	if !rec.hasOutcome("stuck") {
		t.Fatalf("stuck metric missing: %+v", rec.metrics)
	}
}
