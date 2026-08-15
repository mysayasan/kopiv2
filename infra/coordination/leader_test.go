package coordination

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// waitFor polls until cond holds or the deadline passes, so the tests do not depend
// on a fixed sleep matching the campaign tick.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func testLeaderOpts(retry time.Duration) LeaderOptions {
	return LeaderOptions{Resource: "test-leader", Retry: retry}
}

// The single-process case, which is every install that never configured Redis. It
// must be in charge immediately and stay that way, or all background work stops.
func TestMemoryLockerElectsTheOnlyProcess(t *testing.T) {
	locker := NewMemoryLocker(Config{AppName: "test"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	leader := Elect(ctx, locker, testLeaderOpts(10*time.Millisecond))
	waitFor(t, 2*time.Second, "the lone process to lead", leader.IsLeader)

	// And it must not flap: re-verification each tick has to keep confirming it.
	time.Sleep(60 * time.Millisecond)
	if !leader.IsLeader() {
		t.Fatal("the only process must stay leader")
	}
}

// The point of the whole exercise: two instances sharing one locker, exactly one of
// which believes it is in charge.
func TestExactlyOneLeaderAcrossTwoInstances(t *testing.T) {
	locker := NewMemoryLocker(Config{AppName: "test"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := Elect(ctx, locker, testLeaderOpts(10*time.Millisecond))
	b := Elect(ctx, locker, testLeaderOpts(10*time.Millisecond))

	waitFor(t, 2*time.Second, "one of the two to lead", func() bool {
		return a.IsLeader() || b.IsLeader()
	})
	// Give the loser several ticks to (wrongly) decide it is also in charge.
	time.Sleep(80 * time.Millisecond)

	if a.IsLeader() && b.IsLeader() {
		t.Fatal("both instances claimed leadership — the work would run twice")
	}
	if !a.IsLeader() && !b.IsLeader() {
		t.Fatal("neither instance claimed leadership — the work would never run")
	}
}

// When the leader goes away its lease must be released, and the follower must pick
// the work up without a restart.
func TestFollowerTakesOverWhenLeaderStops(t *testing.T) {
	locker := NewMemoryLocker(Config{AppName: "test"}, nil)
	leaderCtx, stopLeader := context.WithCancel(context.Background())
	followerCtx, stopFollower := context.WithCancel(context.Background())
	defer stopFollower()

	a := Elect(leaderCtx, locker, testLeaderOpts(10*time.Millisecond))
	waitFor(t, 2*time.Second, "the first instance to lead", a.IsLeader)

	b := Elect(followerCtx, locker, testLeaderOpts(10*time.Millisecond))
	time.Sleep(50 * time.Millisecond)
	if b.IsLeader() {
		t.Fatal("the follower must not lead while the leader holds the lease")
	}

	stopLeader()
	waitFor(t, 2*time.Second, "the follower to take over", b.IsLeader)
	if a.IsLeader() {
		t.Fatal("a stopped instance must not still consider itself leader")
	}
}

// A lease can be lost without anyone being told. The holder has to notice and stand
// down, or it becomes a silent second writer alongside its replacement.
func TestLeaderStandsDownWhenLeaseIsLost(t *testing.T) {
	locker := &flakyLocker{inner: NewMemoryLocker(Config{AppName: "test"}, nil)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	leader := Elect(ctx, locker, testLeaderOpts(10*time.Millisecond))
	waitFor(t, 2*time.Second, "initial leadership", leader.IsLeader)

	// Simulate the lease expiring underneath the holder while it still has the object.
	locker.invalidate(true)
	locker.blockAcquire(true)
	waitFor(t, 2*time.Second, "the leader to stand down", func() bool { return !leader.IsLeader() })

	// And it must recover once the store is healthy again.
	locker.invalidate(false)
	locker.blockAcquire(false)
	waitFor(t, 2*time.Second, "leadership to be regained", leader.IsLeader)
}

// An unreachable coordination store must read as "not in charge". Failing closed
// costs a skipped round; failing open runs the work everywhere at once, during
// exactly the partition where that does the most damage.
func TestUnreachableLockerIsNotLeader(t *testing.T) {
	locker := &flakyLocker{inner: NewMemoryLocker(Config{AppName: "test"}, nil)}
	locker.failAcquire(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	leader := Elect(ctx, locker, testLeaderOpts(10*time.Millisecond))
	time.Sleep(60 * time.Millisecond)
	if leader.IsLeader() {
		t.Fatal("an instance that cannot reach the lock store must not lead")
	}
}

// A caller that was never wired for coordination is, by definition, the only one.
func TestNilLockerAndNilLeaderLead(t *testing.T) {
	leader := Elect(context.Background(), nil, LeaderOptions{})
	if !leader.IsLeader() {
		t.Fatal("a Leader with no locker must lead")
	}
	var absent *Leader
	if !absent.IsLeader() {
		t.Fatal("a nil Leader must lead, or unwired background work silently stops")
	}
}

func TestElectNotifiesOnTransitionsOnly(t *testing.T) {
	locker := NewMemoryLocker(Config{AppName: "test"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var changes []bool
	opts := testLeaderOpts(10 * time.Millisecond)
	opts.OnChange = func(isLeader bool) {
		mu.Lock()
		changes = append(changes, isLeader)
		mu.Unlock()
	}

	leader := Elect(ctx, locker, opts)
	waitFor(t, 2*time.Second, "leadership", leader.IsLeader)
	time.Sleep(80 * time.Millisecond) // many ticks, all confirming the same state

	mu.Lock()
	defer mu.Unlock()
	if len(changes) != 1 || !changes[0] {
		t.Fatalf("expected exactly one gained-leadership notification, got %v", changes)
	}
}

// TryLock must never queue: the loser has to be told no immediately, because the
// whole point is that it SKIPS the work rather than doing it late.
func TestTryLockDoesNotQueue(t *testing.T) {
	locker := NewMemoryLocker(Config{AppName: "test"}, nil)
	ctx := context.Background()

	first, err := locker.TryLock(ctx, "res")
	if err != nil {
		t.Fatalf("first TryLock: %v", err)
	}
	start := time.Now()
	if _, err := locker.TryLock(ctx, "res"); !errors.Is(err, ErrNotAcquired) {
		t.Fatalf("second TryLock: got %v, want ErrNotAcquired", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("TryLock queued for %v instead of failing fast", elapsed)
	}

	if !first.Valid(ctx) {
		t.Fatal("the holder's lock should still be valid")
	}
	if err := first.Release(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
	if first.Valid(ctx) {
		t.Fatal("a released lock must not report as valid")
	}
	// Released means available again.
	if _, err := locker.TryLock(ctx, "res"); err != nil {
		t.Fatalf("TryLock after release: %v", err)
	}
}

// flakyLocker wraps a real locker so a test can simulate a lease vanishing and a
// store becoming unreachable.
type flakyLocker struct {
	inner   Locker
	mu      sync.Mutex
	invalid bool
	blocked bool
	failing bool
}

func (f *flakyLocker) invalidate(v bool)   { f.mu.Lock(); f.invalid = v; f.mu.Unlock() }
func (f *flakyLocker) blockAcquire(v bool) { f.mu.Lock(); f.blocked = v; f.mu.Unlock() }
func (f *flakyLocker) failAcquire(v bool)  { f.mu.Lock(); f.failing = v; f.mu.Unlock() }

func (f *flakyLocker) Lock(ctx context.Context, resource string) (Lock, error) {
	return f.inner.Lock(ctx, resource)
}

func (f *flakyLocker) TryLock(ctx context.Context, resource string) (Lock, error) {
	f.mu.Lock()
	failing, blocked := f.failing, f.blocked
	f.mu.Unlock()
	if failing {
		return nil, errors.New("lock store unreachable")
	}
	if blocked {
		return nil, ErrNotAcquired
	}
	lock, err := f.inner.TryLock(ctx, resource)
	if err != nil {
		return nil, err
	}
	return &flakyLock{Lock: lock, parent: f}, nil
}

func (f *flakyLocker) Ping(ctx context.Context) error { return f.inner.Ping(ctx) }
func (f *flakyLocker) Close() error                   { return f.inner.Close() }

type flakyLock struct {
	Lock
	parent *flakyLocker
}

func (l *flakyLock) Valid(ctx context.Context) bool {
	l.parent.mu.Lock()
	invalid := l.parent.invalid
	l.parent.mu.Unlock()
	if invalid {
		return false
	}
	return l.Lock.Valid(ctx)
}
