package safego

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A recovered panic must be COUNTED, not merely logged — that is the whole point of the
// observer. A supervised loop that panics and restarts leaves no other symptom: the process is
// up, the API answers, and the subsystem it drove has silently stopped. If the observer is ever
// unwired, this test fails.
func TestPanicObserver_CountsEveryRecoveredPanic(t *testing.T) {
	var count int64
	SetPanicObserver(func(component string) {
		if component != "boom" {
			t.Errorf("expected the task name, got %q", component)
		}
		atomic.AddInt64(&count, 1)
	})
	defer SetPanicObserver(nil)
	// Keep panics off the test log.
	SetLogger(func(string, string, ...any) {})
	defer SetLogger(nil)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)

	panics := 0
	Supervise(ctx, "boom", func(context.Context) {
		// Panic the first three times it is run, then stop cleanly so the loop ends.
		if panics < 3 {
			panics++
			panic("crash-loop")
		}
		cancel()
		wg.Done()
	})

	// Wait for the loop to settle. The backoff between restarts is small at first.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("supervised task did not settle in time")
	}

	if got := atomic.LoadInt64(&count); got != 3 {
		t.Fatalf("expected 3 counted panics, got %d — a crash-looping task must be visible", got)
	}
}

// A one-shot Go() that panics is also counted: it will not be restarted (that is Go's contract),
// but it still died, and a task that was supposed to run once and instead panicked is worth a
// number.
func TestPanicObserver_CountsAOneShotPanic(t *testing.T) {
	var count int64
	SetPanicObserver(func(string) { atomic.AddInt64(&count, 1) })
	defer SetPanicObserver(nil)
	SetLogger(func(string, string, ...any) {})
	defer SetLogger(nil)

	done := make(chan struct{})
	Go("once", func() {
		defer close(done)
		panic("boom")
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("one-shot did not run")
	}
	// Give the deferred recover a moment.
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt64(&count) != 1 {
		t.Fatalf("a one-shot panic must still be counted, got %d", atomic.LoadInt64(&count))
	}
}
