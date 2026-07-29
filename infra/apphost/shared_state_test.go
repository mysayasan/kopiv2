package apphost

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func captureBoot(t *testing.T, cacheProvider, lockProvider string) string {
	t.Helper()
	var buf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})

	warnSharedStateBoundary(cacheProvider, lockProvider)
	return buf.String()
}

// The default shipped config is an in-process cache, and nothing told a myidsan operator
// that it means single-instance-only. The failure it causes — users signed out whenever the
// load balancer moves them — reads as flaky sign-ins, not as a config mistake, so the
// boundary has to be stated at boot where it can actually be found.
func TestMemoryCacheStatesTheSingleInstanceBoundary(t *testing.T) {
	out := captureBoot(t, "inmemory", "memory")

	if !strings.Contains(out, "SINGLE-INSTANCE ONLY") {
		t.Fatalf("a per-process cache must say so plainly; got: %q", out)
	}
	// It must also say what to DO, or it is just an alarming line in a log.
	if !strings.Contains(out, "redis") {
		t.Fatalf("the message must name the fix; got: %q", out)
	}
}

// The loud case: a distributed lock only makes sense with more than one instance, so
// pairing it with a per-process session cache is a self-inconsistent deployment — a fact
// about the config, not a guess about the topology.
func TestDistributedLockWithMemoryCacheIsWarnedAsInconsistent(t *testing.T) {
	out := captureBoot(t, "inmemory", "redis")

	if !strings.Contains(out, "WARNING") {
		t.Fatalf("a distributed lock beside a per-process cache must warn; got: %q", out)
	}
	if !strings.Contains(out, "inconsistent") {
		t.Fatalf("the warning must name the contradiction rather than just complain; got: %q", out)
	}
	// It has to explain the consequence, which is what makes an operator act on it.
	if !strings.Contains(strings.ToLower(out), "signed out") {
		t.Fatalf("the warning must state the user-visible consequence; got: %q", out)
	}
}

// A correctly configured multi-instance deployment must NOT be warned at. A warning that
// fires on a healthy config is one operators learn to ignore, which costs the real one.
func TestSharedCacheIsNotWarnedAbout(t *testing.T) {
	out := captureBoot(t, "redis", "redis")

	if strings.Contains(out, "WARNING") {
		t.Fatalf("a fully shared deployment must not warn; got: %q", out)
	}
	if strings.Contains(out, "SINGLE-INSTANCE ONLY") {
		t.Fatalf("a shared cache is not single-instance; got: %q", out)
	}
	if !strings.Contains(out, "load balancer") {
		t.Fatalf("the healthy case should still confirm what it supports; got: %q", out)
	}
}

// Redis as the cache is what makes the deployment shareable, regardless of the lock: an
// operator using the in-memory lock with a shared cache has a working single-instance-plus
// setup and should not be told they are single-instance.
func TestSharedCacheWithMemoryLockIsStillShared(t *testing.T) {
	out := captureBoot(t, "redis", "memory")

	if strings.Contains(out, "SINGLE-INSTANCE ONLY") || strings.Contains(out, "WARNING") {
		t.Fatalf("the cache decides shareability of sessions; got: %q", out)
	}
}

func TestProviderClassification(t *testing.T) {
	for provider, want := range map[string]bool{
		"redis":         true,
		"Redis":         true,
		"  redis  ":     true,
		"redis-cluster": true,
		"inmemory":      false,
		"memory":        false,
		"default":       false,
		"":              false,
	} {
		if got := isSharedCacheProvider(provider); got != want {
			t.Fatalf("isSharedCacheProvider(%q) = %v want %v", provider, got, want)
		}
	}
}
