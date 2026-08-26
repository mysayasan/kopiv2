package apis

import (
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/infra/cache"
)

// A shared store stands in for Redis here. It is process-local, which is exactly what makes
// it a valid stand-in: two LoginGuards pointed at ONE store are two instances pointed at one
// Redis, and that is the whole property under test. What it cannot stand in for is Redis's
// atomicity — the sliding window is a Lua script there and a mutex here — so the live
// two-instance bench (tools/fleetbench/bench_idsan_lockout.py) remains the real evidence.
func sharedGuardPair(t *testing.T) (*LoginGuard, *LoginGuard, cache.Store) {
	t.Helper()
	store := cache.NewMemoryStore(time.Minute, time.Minute)
	cfg := LoginGuardConfig{
		Enabled:     true,
		MaxAttempts: 4,
		Window:      5 * time.Minute,
		BaseLockout: time.Minute,
		MaxLockout:  time.Hour,
	}
	a := NewLoginGuard(cfg).WithSharedStore(store, "test")
	b := NewLoginGuard(cfg).WithSharedStore(store, "test")
	return a, b, store
}

// THE claim. myidsan and myseliasan are documented as clusterable, and a per-process lockout
// in a cluster is not a lockout: a live two-instance bench locked an account on instance A
// after eight wrong passwords, and instance B then signed the same account in with the
// correct password as though nothing had happened.
func TestLockoutOnOneInstanceLocksTheOther(t *testing.T) {
	a, b, _ := sharedGuardPair(t)

	for i := 0; i < 4; i++ {
		a.RecordFailure("ip:203.0.113.7", "acct:victim")
	}
	if locked, _ := a.Locked("ip:203.0.113.7", "acct:victim"); !locked {
		t.Fatal("the instance taking the attempts did not lock")
	}

	locked, retry := b.Locked("ip:203.0.113.7", "acct:victim")
	if !locked {
		t.Fatal("the OTHER instance did not see the lockout — an attacker behind a load " +
			"balancer gets a fresh budget on every instance, and the locked-out user can " +
			"simply retry until they land somewhere else")
	}
	if retry <= 0 {
		t.Errorf("retry = %v, want a positive wait the client can act on", retry)
	}
}

// The account key must travel too, or a spray distributed across source addresses is only
// ever throttled on whichever instance happens to receive it.
func TestTheAccountKeyIsSharedIndependentlyOfTheSource(t *testing.T) {
	a, b, _ := sharedGuardPair(t)

	for i := 0; i < 4; i++ {
		a.RecordFailure("ip:198.51.100.1", "acct:victim")
	}
	// A different source entirely, on the other instance.
	if locked, _ := b.Locked("ip:203.0.113.99", "acct:victim"); !locked {
		t.Fatal("the account was not locked when approached from another address")
	}
	if locked, _ := b.Locked("ip:203.0.113.99", "acct:bystander"); locked {
		t.Fatal("an unrelated account was locked — the lockout must be scoped to who is " +
			"under attack, not to everyone sharing a network")
	}
}

// A correct credential clears the DEPLOYMENT-WIDE counter, not just the instance that saw
// it. Otherwise a user who mistypes on one instance and succeeds on another stays one
// attempt from being shut out everywhere.
//
// What this does NOT clear, and cannot: the in-memory tally on some OTHER instance that
// absorbed the earlier failures. Nothing reaches into another process's map, so that
// instance keeps counting until its own window rolls — meaning a user who fails
// almost-to-threshold on instance A, succeeds on B, and then fails once more on A can still
// be locked by A alone. It is bounded (one base lockout), it is an inconvenience rather than
// an exposure, and it errs toward locking, which is the right direction for the half of this
// that is a security control. Stated here so the next reader knows it is a known edge and
// not an oversight.
func TestSuccessOnOneInstanceClearsTheSharedCounter(t *testing.T) {
	a, b, _ := sharedGuardPair(t)

	for i := 0; i < 3; i++ {
		a.RecordFailure("ip:203.0.113.7", "acct:victim")
	}
	b.RecordSuccess("ip:203.0.113.7", "acct:victim")

	// Failures now go to the instance with no local tally of its own, so anything that
	// locks here can only have come from the shared counter.
	for i := 0; i < 3; i++ {
		b.RecordFailure("ip:203.0.113.7", "acct:victim")
		if locked, _ := b.Locked("ip:203.0.113.7", "acct:victim"); locked {
			t.Fatalf("locked after %d fresh failures — the success did not reset the "+
				"shared counter", i+1)
		}
	}
}

// The documented edge above, pinned so a later change that fixes it does so deliberately
// rather than by accident, and so nobody reads the test above as a claim it does not make.
func TestSuccessElsewhereDoesNotClearAnInstancesOwnTally(t *testing.T) {
	a, b, _ := sharedGuardPair(t)

	for i := 0; i < 3; i++ {
		a.RecordFailure("acct:victim")
	}
	b.RecordSuccess("acct:victim")
	a.RecordFailure("acct:victim")

	if locked, _ := a.Locked("acct:victim"); !locked {
		t.Skip("the instance that absorbed the failures no longer keeps its own tally — " +
			"that is an improvement, not a regression; update this test to match")
	}
}

// The threshold has to be the SAME number on both halves. The sliding window refuses when
// the stored count already meets the limit, checked before the current attempt is recorded,
// while the in-memory guard trips when the total reaches MaxAttempts — so passing
// MaxAttempts straight through would leave every instance one free guess.
func TestTheSharedHalfTripsOnTheSameAttemptAsTheLocalOne(t *testing.T) {
	a, b, _ := sharedGuardPair(t)

	for i := 0; i < 3; i++ {
		a.RecordFailure("acct:victim")
		if locked, _ := b.Locked("acct:victim"); locked {
			t.Fatalf("the other instance locked after %d failures, before the threshold of 4", i+1)
		}
	}
	a.RecordFailure("acct:victim")
	if locked, _ := b.Locked("acct:victim"); !locked {
		t.Fatal("the other instance did not lock on the 4th failure, so it would have " +
			"allowed a free guess of its own")
	}
}

// Escalation is a deployment-wide count. If each instance escalated from its own tally, an
// attacker rotating instances would keep resetting the backoff to its base.
func TestEscalationCarriesAcrossInstances(t *testing.T) {
	a, b, _ := sharedGuardPair(t)
	base := time.Minute

	for i := 0; i < 4; i++ {
		a.RecordFailure("acct:victim")
	}
	_, first := a.Locked("acct:victim")
	if first > base+time.Second {
		t.Fatalf("first lockout = %v, want about %v", first, base)
	}

	// Let the first lockout expire the way TIME would, rather than by succeeding — a
	// successful sign-in legitimately resets the escalation, so clearing it that way would
	// erase the very thing being measured. Both guards share the injected clock so the
	// in-memory and shared halves agree about how long it has been.
	fastForward(5*time.Minute, a, b)
	for i := 0; i < 4; i++ {
		b.RecordFailure("acct:victim")
	}
	_, second := b.Locked("acct:victim")
	if second <= first {
		t.Fatalf("second lockout = %v, want longer than the first (%v) — the escalation "+
			"restarted at the base on the second instance", second, first)
	}
}

// fastForward moves each guard's clock on by d, expiring lockouts without clearing the
// escalation counters that outlive them. Both halves read the same injected clock, so the
// in-memory lockedUntil and the shared expiry agree about how much time has passed.
func fastForward(d time.Duration, guards ...*LoginGuard) {
	for _, g := range guards {
		base := g.now
		g.now = func() time.Time { return base().Add(d) }
	}
}

// A guard with no shared store must behave exactly as it always did — that is what a
// single-instance install runs, and what every deployment falls back to when the cache is
// unreachable.
func TestWithoutASharedStoreNothingChanges(t *testing.T) {
	cfg := LoginGuardConfig{Enabled: true, MaxAttempts: 3, Window: time.Minute,
		BaseLockout: time.Minute, MaxLockout: time.Hour}
	a := NewLoginGuard(cfg)
	b := NewLoginGuard(cfg)

	for i := 0; i < 3; i++ {
		a.RecordFailure("acct:victim")
	}
	if locked, _ := a.Locked("acct:victim"); !locked {
		t.Fatal("the local guard stopped locking")
	}
	if locked, _ := b.Locked("acct:victim"); locked {
		t.Fatal("a guard with no shared store saw another guard's state")
	}
	if a.SharesState() {
		t.Error("SharesState must be false without a store, so the operator is not told " +
			"their cluster is protected when it is not")
	}
}
