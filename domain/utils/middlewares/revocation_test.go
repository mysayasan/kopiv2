package middlewares

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// A definite "not active" must end the session. This is the whole point.
func TestRevocationCheckerRefusesRevokedSession(t *testing.T) {
	c := NewRevocationChecker(func(context.Context, string) (bool, bool) { return false, true }, time.Minute)
	if c.stillActive(context.Background(), nil, &SessionCacheEntry{SessionId: "sid"}) {
		t.Fatal("a session the identity server reports as inactive must not keep being served")
	}
}

// An UNREACHABLE identity server is not a revocation. Failing closed here would sign the whole
// estate out of the fleet console every time myidsan restarts — the tool people need most
// during exactly that kind of incident.
func TestRevocationCheckerFailsOpenWhenUnreachable(t *testing.T) {
	c := NewRevocationChecker(func(context.Context, string) (bool, bool) { return false, false }, time.Minute)
	if !c.stillActive(context.Background(), nil, &SessionCacheEntry{SessionId: "sid"}) {
		t.Fatal("an unreachable identity server must not be treated as a revocation")
	}
}

// And it must not remember the non-answer as if it had been checked: recovery has to be
// immediate once the identity server responds again, not delayed by a whole interval.
func TestRevocationCheckerDoesNotCacheANonAnswer(t *testing.T) {
	var calls int32
	c := NewRevocationChecker(func(context.Context, string) (bool, bool) {
		atomic.AddInt32(&calls, 1)
		return false, false
	}, time.Hour)
	entry := &SessionCacheEntry{SessionId: "sid"}
	c.stillActive(context.Background(), nil, entry)
	c.stillActive(context.Background(), nil, entry)
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("asked %d times, want 2 — an unanswered check must be retried, not cached", got)
	}
}

// A positive answer IS cached, for the interval. Asking per request would put an HTTP round
// trip in front of every API call.
func TestRevocationCheckerRateLimitsPositiveAnswers(t *testing.T) {
	var calls int32
	c := NewRevocationChecker(func(context.Context, string) (bool, bool) {
		atomic.AddInt32(&calls, 1)
		return true, true
	}, time.Hour)
	entry := &SessionCacheEntry{SessionId: "sid"}
	for i := 0; i < 5; i++ {
		if !c.stillActive(context.Background(), nil, entry) {
			t.Fatal("an active session must keep being served")
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("asked %d times, want 1 — positive answers are trusted for the interval", got)
	}
}

// Two sessions must not share one verdict: a revoked session id must not be kept alive by a
// different session having been checked recently.
func TestRevocationCheckerIsPerSession(t *testing.T) {
	c := NewRevocationChecker(func(_ context.Context, sid string) (bool, bool) {
		return sid != "revoked", true
	}, time.Hour)
	if !c.stillActive(context.Background(), nil, &SessionCacheEntry{SessionId: "good"}) {
		t.Fatal("the live session must be served")
	}
	if c.stillActive(context.Background(), nil, &SessionCacheEntry{SessionId: "revoked"}) {
		t.Fatal("the revoked session must be refused even though another was just checked")
	}
}

// A nil checker, or one built without an Ask, must be completely inert — an app that has not
// wired this behaves exactly as it did before.
func TestRevocationCheckerNilIsInert(t *testing.T) {
	if NewRevocationChecker(nil, time.Minute) != nil {
		t.Fatal("no ask function means no checker")
	}
	if NewRevocationChecker(func(context.Context, string) (bool, bool) { return false, true }, 0) != nil {
		t.Fatal("a non-positive interval means no checker")
	}
	var c *RevocationChecker
	if !c.stillActive(context.Background(), nil, &SessionCacheEntry{SessionId: "sid"}) {
		t.Fatal("a nil checker must never refuse anything")
	}
}

// An entry with no session id cannot be asked about, and must not be refused for it.
func TestRevocationCheckerIgnoresSessionlessEntries(t *testing.T) {
	c := NewRevocationChecker(func(context.Context, string) (bool, bool) {
		t.Fatal("must not ask about an entry with no session id")
		return false, true
	}, time.Minute)
	if !c.stillActive(context.Background(), nil, &SessionCacheEntry{}) {
		t.Fatal("an entry with no session id must be left alone")
	}
}
