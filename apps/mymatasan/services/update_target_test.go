package services

import (
	"context"
	"strings"
	"testing"
)

// newTestUpdater builds an updater pinned to a known current version. homeDir is empty, so
// nothing here ever touches the filesystem — these tests are about the decisions made
// BEFORE a byte is downloaded, which is exactly where the guards have to be.
func newTestUpdater(current string) *UpdateService {
	return NewUpdateService(current, "", nil)
}

// TestStartUpdateToRefusesADowngrade is the guard that keeps a staged rollout from turning
// a bad release into a corrupted appliance. Swapping the binary back is easy; the database
// it then opens is not, because migrations here are forward-only. So the refusal happens
// before anything is overwritten, and it says why.
func TestStartUpdateToRefusesADowngrade(t *testing.T) {
	u := newTestUpdater("1.128.0")
	err := u.StartUpdateTo(context.Background(), "1.127.0")
	if err == nil {
		t.Fatal("a downgrade was accepted")
	}
	if !strings.Contains(err.Error(), "forward-only") {
		t.Fatalf("error = %q, want it to explain WHY a downgrade is refused", err)
	}
	if u.Status().Applying {
		t.Fatal("a refused downgrade still started an apply")
	}
}

// TestStartUpdateToRefusesTheVersionItIsAlreadyRunning. Reinstalling the running version is
// not harmless: it restarts the appliance, which drops recording, and in a rollout it would
// burn a ring's settle window proving nothing.
func TestStartUpdateToRefusesTheSameVersion(t *testing.T) {
	u := newTestUpdater("1.128.0")
	for _, v := range []string{"1.128.0", "v1.128.0", " 1.128.0 "} {
		err := u.StartUpdateTo(context.Background(), v)
		if err == nil {
			t.Fatalf("reinstalling the running version (%q) was accepted", v)
		}
		// It must say "already running", not "refusing to downgrade from 1.128.0 to
		// 1.128.0" — which is what the downgrade guard alone would produce, and which reads
		// as nonsense to whoever is staring at a halted rollout trying to work out why.
		if !strings.Contains(err.Error(), "already running") {
			t.Fatalf("error for %q = %q, want it to say the node is already on that version", v, err)
		}
	}
}

// TestStartUpdateToRequiresAVersion. An empty target must not fall back to "latest" — that
// silent fallback is precisely what makes a canary meaningless.
func TestStartUpdateToRequiresAVersion(t *testing.T) {
	u := newTestUpdater("1.128.0")
	for _, v := range []string{"", "   ", "v"} {
		if err := u.StartUpdateTo(context.Background(), v); err == nil {
			t.Fatalf("empty target %q was accepted", v)
		} else if !strings.Contains(err.Error(), "version is required") && !strings.Contains(err.Error(), "self-update is not available") {
			t.Fatalf("error for %q = %q, want a missing-version complaint", v, err)
		}
	}
}

// TestSelectAssetsRefusesWithoutATarget guards the layer below: even if a caller got past
// StartUpdateTo, the asset lookup will not resolve "whatever is latest" on its own.
func TestSelectAssetsRefusesWithoutATarget(t *testing.T) {
	u := newTestUpdater("1.128.0")
	_, _, _, err := u.selectAssets(context.Background(), "")
	if err == nil {
		t.Fatal("selectAssets resolved an empty version instead of refusing")
	}
	// Assert the REFUSAL, not merely that something failed. Without the guard this would
	// reach out to the network and fail there too, on a machine with no route out — an
	// error that looks identical and proves nothing.
	if !strings.Contains(err.Error(), "no target version") {
		t.Fatalf("error = %q, want the refusal rather than an incidental network failure", err)
	}
}

// TestVersionGreaterOrdersReleases underpins both guards above; a wrong answer here makes
// the downgrade refusal either useless or a blocker on legitimate upgrades.
func TestVersionGreaterOrdersReleases(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.128.0", "1.127.0", true},
		{"1.127.0", "1.128.0", false},
		{"1.128.0", "1.128.0", false},
		{"2.0.0", "1.999.999", true},
		{"1.128.1", "1.128.0", true},
		// Double-digit minors are where a string compare would get it wrong.
		{"1.9.0", "1.10.0", false},
		{"1.10.0", "1.9.0", true},
	}
	for _, c := range cases {
		if got := versionGreater(c.a, c.b); got != c.want {
			t.Errorf("versionGreater(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
