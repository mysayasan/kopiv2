package apphost

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// resetRestartChainEnv blanks the storm-guard env for a clean starting point;
// t.Setenv restores the originals when the test ends.
func resetRestartChainEnv(t *testing.T) {
	t.Helper()
	t.Setenv("KOPIV2_RESTART_GEN", "")
	t.Setenv("KOPIV2_RESTART_T0", "")
}

func TestAllowSelfRelaunchFreshStart(t *testing.T) {
	resetRestartChainEnv(t)
	if !allowSelfRelaunch() {
		t.Fatal("a fresh start should be allowed to relaunch")
	}
	if got := os.Getenv("KOPIV2_RESTART_GEN"); got != "1" {
		t.Fatalf("expected GEN=1 after first relaunch, got %q", got)
	}
	if os.Getenv("KOPIV2_RESTART_T0") == "" {
		t.Fatal("expected T0 to be stamped")
	}
}

func TestAllowSelfRelaunchHaltsAStorm(t *testing.T) {
	resetRestartChainEnv(t)
	// Already at the cap within the window: the next relaunch must be refused.
	t.Setenv("KOPIV2_RESTART_GEN", strconv.Itoa(selfRelaunchMax))
	t.Setenv("KOPIV2_RESTART_T0", strconv.FormatInt(time.Now().UnixMilli(), 10))
	if allowSelfRelaunch() {
		t.Fatalf("relaunch #%d within the window should be refused", selfRelaunchMax+1)
	}
}

func TestAllowSelfRelaunchAllowsUpToCap(t *testing.T) {
	resetRestartChainEnv(t)
	t.Setenv("KOPIV2_RESTART_GEN", strconv.Itoa(selfRelaunchMax-1))
	t.Setenv("KOPIV2_RESTART_T0", strconv.FormatInt(time.Now().UnixMilli(), 10))
	if !allowSelfRelaunch() {
		t.Fatalf("relaunch #%d (== cap) should still be allowed", selfRelaunchMax)
	}
}

func TestAllowSelfRelaunchResetsAfterWindow(t *testing.T) {
	resetRestartChainEnv(t)
	// A high count but an old window (the app stayed up a while): the count resets,
	// so a deliberate later restart is never throttled.
	t.Setenv("KOPIV2_RESTART_GEN", "99")
	old := time.Now().Add(-(selfRelaunchWindow + 10*time.Second)).UnixMilli()
	t.Setenv("KOPIV2_RESTART_T0", strconv.FormatInt(old, 10))
	if !allowSelfRelaunch() {
		t.Fatal("relaunch after the window elapsed should be allowed")
	}
	if got := os.Getenv("KOPIV2_RESTART_GEN"); got != "1" {
		t.Fatalf("expected GEN to reset to 1, got %q", got)
	}
}

func TestSupervisedRestartEnv(t *testing.T) {
	// platformSupervised() is false in a normal test process (not a Windows service),
	// so KOPIV2_SUPERVISED alone decides here.
	t.Setenv("KOPIV2_SUPERVISED", "")
	if supervisedRestart() {
		t.Fatal("not supervised without KOPIV2_SUPERVISED (in a non-service test process)")
	}
	t.Setenv("KOPIV2_SUPERVISED", "1")
	if !supervisedRestart() {
		t.Fatal("KOPIV2_SUPERVISED=1 should mark the run supervised")
	}
}
