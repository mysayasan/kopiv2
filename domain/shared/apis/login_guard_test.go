package apis

import (
	"testing"
	"time"
)

// newTestGuard builds a guard with a controllable clock for deterministic tests.
func newTestGuard(cfg LoginGuardConfig, clock *time.Time) *LoginGuard {
	g := NewLoginGuard(cfg)
	g.now = func() time.Time { return *clock }
	return g
}

func TestLoginGuardLocksAfterMaxAttempts(t *testing.T) {
	now := time.Now()
	g := newTestGuard(LoginGuardConfig{
		Enabled:     true,
		MaxAttempts: 3,
		Window:      5 * time.Minute,
		BaseLockout: time.Minute,
		MaxLockout:  15 * time.Minute,
	}, &now)

	for i := 0; i < 2; i++ {
		if lockedNow, _ := g.RecordFailure("ip:1.2.3.4"); lockedNow {
			t.Fatalf("attempt %d should not lock yet", i+1)
		}
	}
	lockedNow, retry := g.RecordFailure("ip:1.2.3.4")
	if !lockedNow {
		t.Fatal("third failure should trip the lockout")
	}
	if retry != time.Minute {
		t.Fatalf("first lockout = %s, want 1m", retry)
	}
	if locked, _ := g.Locked("ip:1.2.3.4"); !locked {
		t.Fatal("key should report locked")
	}

	// A different key is unaffected.
	if locked, _ := g.Locked("ip:9.9.9.9"); locked {
		t.Fatal("unrelated key should not be locked")
	}
}

func TestLoginGuardEscalatingBackoff(t *testing.T) {
	now := time.Now()
	g := newTestGuard(LoginGuardConfig{
		Enabled:     true,
		MaxAttempts: 1,
		Window:      5 * time.Minute,
		BaseLockout: time.Minute,
		MaxLockout:  10 * time.Minute,
	}, &now)

	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 10 * time.Minute, 10 * time.Minute}
	for i, w := range want {
		lockedNow, retry := g.RecordFailure("user:admin")
		if !lockedNow {
			t.Fatalf("lockout %d did not trip", i+1)
		}
		if retry != w {
			t.Fatalf("lockout %d = %s, want %s", i+1, retry, w)
		}
		// Advance past this lockout so the next failure can trip again.
		now = now.Add(retry + time.Second)
	}
}

func TestLoginGuardSuccessClears(t *testing.T) {
	now := time.Now()
	g := newTestGuard(LoginGuardConfig{
		Enabled:     true,
		MaxAttempts: 3,
		Window:      5 * time.Minute,
		BaseLockout: time.Minute,
		MaxLockout:  15 * time.Minute,
	}, &now)

	g.RecordFailure("ip:1.2.3.4")
	g.RecordFailure("ip:1.2.3.4")
	g.RecordSuccess("ip:1.2.3.4")
	// Counter reset: it should take a full MaxAttempts run to lock again.
	for i := 0; i < 2; i++ {
		if lockedNow, _ := g.RecordFailure("ip:1.2.3.4"); lockedNow {
			t.Fatalf("attempt %d locked too early after success reset", i+1)
		}
	}
}

func TestLoginGuardWindowResets(t *testing.T) {
	now := time.Now()
	g := newTestGuard(LoginGuardConfig{
		Enabled:     true,
		MaxAttempts: 3,
		Window:      time.Minute,
		BaseLockout: time.Minute,
		MaxLockout:  15 * time.Minute,
	}, &now)

	g.RecordFailure("ip:1.2.3.4")
	g.RecordFailure("ip:1.2.3.4")
	// Let the window lapse; the stale failures should age out.
	now = now.Add(2 * time.Minute)
	if lockedNow, _ := g.RecordFailure("ip:1.2.3.4"); lockedNow {
		t.Fatal("failures from a lapsed window should not accumulate")
	}
}

func TestLoginGuardDisabledIsInert(t *testing.T) {
	now := time.Now()
	g := newTestGuard(LoginGuardConfig{Enabled: false, MaxAttempts: 1}, &now)
	for i := 0; i < 10; i++ {
		if lockedNow, _ := g.RecordFailure("ip:1.2.3.4"); lockedNow {
			t.Fatal("disabled guard must never lock")
		}
	}
	if locked, _ := g.Locked("ip:1.2.3.4"); locked {
		t.Fatal("disabled guard must never report locked")
	}
}
