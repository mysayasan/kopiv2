package coordination

import (
	"context"
	"sync"
	"time"
)

// Leadership exists to answer one question for background work: am I the instance
// that should do this?
//
// Almost everything an app schedules — retention purges, notification rollups, the
// daily digest, heartbeat reconciliation — is work that must happen ONCE for the
// deployment, not once per process. Run standalone that distinction is invisible and
// costs nothing to ignore. Behind a load balancer it is the difference between a
// rollup that counts each event once and one that counts it N times, and between one
// digest each morning and one per replica.
//
// The design is a lease rather than a queue. A losing instance must get on with not
// doing the work; queueing would hand it the lock the moment the leader finished and
// it would then run exactly the job the lock was meant to deduplicate. See
// Locker.TryLock.
//
// With the memory provider the single process always wins immediately, so standalone
// behaviour is unchanged and no caller needs a provider-specific branch.

// defaultLeaderRetry is how often a follower re-attempts the lease, and how often a
// leader re-verifies it still holds it. It is deliberately shorter than the default
// 10s lease so a leader notices a lost lease before its replacement has been running
// long, and a follower takes over promptly after a leader dies.
const defaultLeaderRetry = 3 * time.Second

// LeaderResource is the lock resource name used for an app's single background
// leader. Exported so an operator reading Redis keys can recognise it.
const LeaderResource = "leader"

// LeaderOptions configures Elect. Every field is optional.
type LeaderOptions struct {
	// Resource names the lease. Defaults to LeaderResource. Use a distinct name only
	// to elect a leader for something narrower than "this app's background work".
	Resource string
	// Retry overrides how often the lease is attempted and re-verified.
	Retry time.Duration
	// OnChange is called whenever leadership is gained or lost, in its own goroutine.
	// Useful for a log line or a metric; it must not block.
	OnChange func(isLeader bool)
}

// Leader tracks whether this instance currently holds the background-work lease.
type Leader struct {
	locker   Locker
	resource string
	retry    time.Duration
	onChange func(bool)

	mu       sync.RWMutex
	held     Lock
	isLeader bool
}

// Elect starts campaigning for leadership and returns immediately. The campaign runs
// until ctx is cancelled, at which point the lease is released so a surviving
// instance can take over promptly rather than waiting out the TTL.
//
// A nil locker yields a Leader that is always in charge — the honest answer for a
// process with no coordination configured, which is by definition the only one.
func Elect(ctx context.Context, locker Locker, opts LeaderOptions) *Leader {
	l := &Leader{
		locker:   locker,
		resource: opts.Resource,
		retry:    opts.Retry,
		onChange: opts.OnChange,
	}
	if l.resource == "" {
		l.resource = LeaderResource
	}
	if l.retry <= 0 {
		l.retry = defaultLeaderRetry
	}
	if locker == nil {
		l.set(true)
		return l
	}
	go l.campaign(ctx)
	return l
}

// IsLeader reports whether this instance currently holds the lease. Callers should
// ask each time they are about to do the work rather than caching the answer:
// leadership moves, and the point of asking is to notice.
func (l *Leader) IsLeader() bool {
	if l == nil {
		// A nil Leader means a caller that was never wired for coordination. Standalone
		// is the safe reading — the alternative is background work that silently stops.
		return true
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.isLeader
}

// Resource returns the lock resource this Leader campaigns for.
func (l *Leader) Resource() string {
	if l == nil {
		return LeaderResource
	}
	return l.resource
}

func (l *Leader) campaign(ctx context.Context) {
	ticker := time.NewTicker(l.retry)
	defer ticker.Stop()

	l.attempt(ctx)
	for {
		select {
		case <-ctx.Done():
			l.resign()
			return
		case <-ticker.C:
			l.attempt(ctx)
		}
	}
}

// attempt either takes the lease or, if it already holds it, confirms it still does.
func (l *Leader) attempt(ctx context.Context) {
	l.mu.RLock()
	held := l.held
	l.mu.RUnlock()

	if held != nil {
		if held.Valid(ctx) {
			return
		}
		// The lease was lost without anyone saying so — a stall past the TTL, or the
		// store dropping the key. Stand down before trying again, so the gap is a
		// moment with no leader rather than a moment with two.
		l.mu.Lock()
		l.held = nil
		l.mu.Unlock()
		l.set(false)
		_ = held.Release(ctx)
	}

	lock, err := l.locker.TryLock(ctx, l.resource)
	if err != nil {
		// ErrNotAcquired means somebody else is leading, which is a normal steady
		// state; any other error means the store is unreachable, and the safe reading
		// of "I cannot tell" is that this instance is NOT in charge.
		l.set(false)
		return
	}
	l.mu.Lock()
	l.held = lock
	l.mu.Unlock()
	l.set(true)
}

// resign releases the lease on shutdown so the next instance can take over within a
// tick instead of waiting for the lease to expire.
func (l *Leader) resign() {
	l.mu.Lock()
	held := l.held
	l.held = nil
	l.mu.Unlock()
	if held != nil {
		// A fresh context: the campaign's is already cancelled, and releasing is the
		// one thing still worth doing with it.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = held.Release(ctx)
	}
	l.set(false)
}

// set records the leadership state and notifies on transitions only.
func (l *Leader) set(isLeader bool) {
	l.mu.Lock()
	changed := l.isLeader != isLeader
	l.isLeader = isLeader
	onChange := l.onChange
	l.mu.Unlock()

	if changed && onChange != nil {
		go onChange(isLeader)
	}
}
