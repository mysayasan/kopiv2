package notification

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
)

// gateFixture builds a small, predictable set of notifications plus the repos and
// cursor a maintainer folds them into.
func gateFixture() (*fakeNotifRepo, *fakeRollupRepo, *fakeCursor) {
	const base = int64(1_699_920_000) // hour-aligned, so bucket starts are predictable
	return &fakeNotifRepo{rows: []*entities.Notification{
			{Id: 1, CreatedAt: base + 10, Category: string(CategorySystem), Severity: "info"},
			{Id: 2, CreatedAt: base + 20, Category: string(CategorySystem), Severity: "info"},
		}},
		&fakeRollupRepo{},
		&fakeCursor{}
}

// countingCursor records how many times a sweep began, so a test can assert that a
// gated loop never STARTED one. Asserting on the rollup totals instead would be
// misleading: the shared cursor already makes a sequential second sweep a no-op, so
// the totals look correct whether or not the gate does anything.
//
// The corruption the gate prevents needs two sweeps to OVERLAP — both reading the
// cursor before either writes it, then both folding the same page and incrementing
// every bucket twice. Two instances on independent timers get there on their own; it
// is a race, not a certainty, which is exactly why it would be found late.
type countingCursor struct {
	mu     sync.Mutex
	id     int64
	getter int
}

func (c *countingCursor) Get(context.Context) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.getter++
	return c.id, nil
}

func (c *countingCursor) Set(_ context.Context, lastID int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.id = lastID
	return nil
}

func (c *countingCursor) sweeps() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getter
}

// A follower's loop must never begin a sweep at all.
func TestRollupGateFollowerNeverSweeps(t *testing.T) {
	notifs, rollups, _ := gateFixture()
	cursor := &countingCursor{}

	follower := NewRollupMaintainer(notifs, rollups, cursor, 10*time.Millisecond, 5000).
		WithGate(func() bool { return false })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	follower.Start(ctx)
	// Past firstTickDelay, so many ticks have happened and every one was gated. A
	// shorter window would pass whether or not the gate worked.
	time.Sleep(firstTickDelay + 300*time.Millisecond)

	if got := cursor.sweeps(); got != 0 {
		t.Fatalf("a follower started %d sweeps, want 0", got)
	}
	if got := rollups.totalCount(); got != 0 {
		t.Fatalf("a follower folded %d rows, want 0", got)
	}
}

// A follower must keep ticking rather than exiting, so an instance promoted to leader
// starts sweeping without needing a restart.
func TestRollupGateFollowerResumesAfterPromotion(t *testing.T) {
	notifs, rollups, cursor := gateFixture()

	var mu sync.Mutex
	isLeader := false
	m := NewRollupMaintainer(notifs, rollups, cursor, 10*time.Millisecond, 5000).
		WithGate(func() bool { mu.Lock(); defer mu.Unlock(); return isLeader })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)

	// While a follower: nothing is folded, however long it ticks. Waited past
	// firstTickDelay so at least one tick has actually happened and been gated —
	// otherwise this would pass simply because the loop had not started working yet.
	time.Sleep(firstTickDelay + 300*time.Millisecond)
	if rollups.totalCount() != 0 {
		t.Fatalf("a follower folded %d rows, want 0", rollups.totalCount())
	}

	mu.Lock()
	isLeader = true
	mu.Unlock()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if rollups.totalCount() == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("after promotion the maintainer folded %d rows, want 2 — a demoted loop must not exit", rollups.totalCount())
}

// A single-instance app passes no gate at all, and must be completely unaffected.
func TestRollupNoGateSweepsAsBefore(t *testing.T) {
	notifs, rollups, cursor := gateFixture()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	NewRollupMaintainer(notifs, rollups, cursor, 10*time.Millisecond, 5000).Start(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rollups.totalCount() == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("ungated maintainer folded %d rows, want 2", rollups.totalCount())
}
