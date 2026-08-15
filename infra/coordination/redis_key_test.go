package coordination

import "testing"

// Two DIFFERENT applications sharing one Redis must not share a lock.
//
// Every app in the suite ships the same cache.keyPrefix ("kopiv2"), and every app's
// background leader asks for the same resource name ("leader"). Without the app name in the
// key, pointing two apps at one Redis gives the pair a single leader: the loser stops
// running its retention purges, rollups, digests and reconciliation entirely, and nothing
// logs an error — the work simply never happens.
func TestLockKeysAreNamespacedPerApp(t *testing.T) {
	seliasan := NewRedisLocker(Config{AppName: "myseliasan", KeyPrefix: "kopiv2"}, nil)
	idsan := NewRedisLocker(Config{AppName: "myidsan", KeyPrefix: "kopiv2"}, nil)

	a := seliasan.lockKey(LeaderResource)
	b := idsan.lockKey(LeaderResource)
	if a == b {
		t.Fatalf("two apps share the leader lock key %q — one of them would stop running all background work", a)
	}
	// The queue and wait keys are derived the same way, so they must separate too.
	if seliasan.queueKey("x") == idsan.queueKey("x") {
		t.Fatal("queue keys collide across apps")
	}
	if seliasan.waitKey("x", "tok") == idsan.waitKey("x", "tok") {
		t.Fatal("wait keys collide across apps")
	}
}

// Two instances of the SAME app must still share a lock — that is the entire mechanism.
func TestLockKeysMatchAcrossInstancesOfOneApp(t *testing.T) {
	a := NewRedisLocker(Config{AppName: "myseliasan", KeyPrefix: "kopiv2"}, nil)
	b := NewRedisLocker(Config{AppName: "myseliasan", KeyPrefix: "kopiv2"}, nil)

	if a.lockKey(LeaderResource) != b.lockKey(LeaderResource) {
		t.Fatal("two instances of one app must contend for the SAME leader lock")
	}
}

// The key stays well-formed when either part is missing, so a minimal config cannot
// produce something like "::tx:leader" that is hard to recognise in Redis.
func TestLockKeyShapes(t *testing.T) {
	for _, tc := range []struct{ app, prefix, want string }{
		{"myseliasan", "kopiv2", "kopiv2:myseliasan:tx:lock:leader"},
		{"myseliasan", "", "myseliasan:tx:lock:leader"},
		{"", "kopiv2", "kopiv2:tx:lock:leader"},
		{"", "", "tx:lock:leader"},
	} {
		l := NewRedisLocker(Config{AppName: tc.app, KeyPrefix: tc.prefix}, nil)
		if got := l.lockKey(LeaderResource); got != tc.want {
			t.Errorf("app=%q prefix=%q: got %q, want %q", tc.app, tc.prefix, got, tc.want)
		}
	}
}
