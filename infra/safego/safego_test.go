package safego

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// captureLogs redirects recovered-panic reports for the duration of a test.
func captureLogs(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	lines := []string{}
	SetLogger(func(component, format string, args ...any) {
		mu.Lock()
		lines = append(lines, component+": "+format)
		mu.Unlock()
	})
	t.Cleanup(func() { SetLogger(nil) })
	return &lines
}

// The whole point: a panicking background task must not take the process down.
func TestGo_RecoversPanic(t *testing.T) {
	logs := captureLogs(t)
	done := make(chan struct{})

	Go("boom", func() {
		defer close(done)
		panic("kaboom")
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
	// Give the deferred recover a moment to log.
	time.Sleep(50 * time.Millisecond)
	if len(*logs) == 0 || !strings.Contains((*logs)[0], "boom") {
		t.Fatalf("panic was not reported: %v", *logs)
	}
}

// A supervised loop that panics must come back — recovering without restarting would
// leave the subsystem silently dead (a vision sampler's map entry outlives its
// goroutine, so nothing would ever notice).
func TestSupervise_RestartsAfterPanic(t *testing.T) {
	captureLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runs atomic.Int32
	recovered := make(chan struct{})

	Supervise(ctx, "flaky", func(ctx context.Context) {
		n := runs.Add(1)
		if n == 1 {
			panic("first run dies")
		}
		close(recovered) // the restart happened
		<-ctx.Done()
	})

	select {
	case <-recovered:
	case <-time.After(5 * time.Second):
		t.Fatalf("supervised task was not restarted after panicking (runs=%d)", runs.Load())
	}
}

// A clean return means the task decided it was done (a loop exiting on ctx.Done).
// Restarting it would spin forever.
func TestSupervise_DoesNotRestartOnCleanReturn(t *testing.T) {
	captureLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runs atomic.Int32
	Supervise(ctx, "one-shot", func(ctx context.Context) {
		runs.Add(1)
	})

	time.Sleep(300 * time.Millisecond)
	if got := runs.Load(); got != 1 {
		t.Fatalf("clean return should not be restarted, ran %d times", got)
	}
}

// Cancelling the context must stop the supervisor even while it is backing off, so
// shutdown is never delayed by a task that is mid-restart.
func TestSupervise_StopsOnContextCancel(t *testing.T) {
	captureLogs(t)
	ctx, cancel := context.WithCancel(context.Background())

	var runs atomic.Int32
	started := make(chan struct{}, 1)
	Supervise(ctx, "always-panics", func(ctx context.Context) {
		runs.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		panic("always")
	})

	<-started
	cancel()
	// The first restart waits initialBackoff (1s); after cancel it must not run again.
	after := runs.Load()
	time.Sleep(1500 * time.Millisecond)
	if got := runs.Load(); got > after {
		t.Fatalf("supervisor kept restarting after context cancel: %d -> %d", after, got)
	}
}
