package apphost

import (
	"bytes"
	"log"
	"strings"
	"testing"

	sharedServices "github.com/mysayasan/kopiv2/domain/shared/services"
	"github.com/mysayasan/kopiv2/infra/atrest"
)

func captureBootMode(t *testing.T, cacheProvider, lockProvider, declaredMode string) string {
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

	warnSharedStateBoundary(cacheProvider, lockProvider, declaredMode)
	return buf.String()
}

// captureBoot covers the undeclared case — an install that predates the wizard, or one
// booting before the operator has answered. Inference from config is all there is.
func captureBoot(t *testing.T, cacheProvider, lockProvider string) string {
	t.Helper()
	return captureBootMode(t, cacheProvider, lockProvider, "")
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

// The declared answer removes the guesswork. An operator who told the wizard this is a
// multi-instance install and then left the per-process cache in place is not an ambiguous
// topology to infer — it is simply wrong, and the boot line says so without hedging.
func TestDeclaredClusteredWithMemoryCacheIsWarned(t *testing.T) {
	out := captureBootMode(t, "inmemory", "redis", sharedServices.ModeClustered)

	if !strings.Contains(out, "WARNING") {
		t.Fatalf("a declared cluster on a per-process cache must warn; got: %q", out)
	}
	if !strings.Contains(out, "DECLARED") {
		t.Fatalf("the warning should rest on the declared intent, not an inference; got: %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "signed out") {
		t.Fatalf("the warning must state the user-visible consequence; got: %q", out)
	}
}

// Scheduled work is serialized by the transaction lock, so a declared cluster with a
// per-process lock runs every purge, rollup and digest on every instance. That is a
// different failure from the session one and needs its own line.
func TestDeclaredClusteredWithMemoryLockIsWarned(t *testing.T) {
	out := captureBootMode(t, "redis", "memory", sharedServices.ModeClustered)

	if !strings.Contains(out, "WARNING") {
		t.Fatalf("a declared cluster on a per-process lock must warn; got: %q", out)
	}
	if !strings.Contains(out, "lockProvider") {
		t.Fatalf("the warning must name the setting to change; got: %q", out)
	}
}

func TestDeclaredClusteredFullyConfiguredIsConfirmed(t *testing.T) {
	out := captureBootMode(t, "redis", "redis", sharedServices.ModeClustered)

	if strings.Contains(out, "WARNING") {
		t.Fatalf("a correctly configured declared cluster must not warn; got: %q", out)
	}
	if !strings.Contains(out, "load balancer") {
		t.Fatalf("the healthy case should confirm what it supports; got: %q", out)
	}
}

// An explicit single-instance answer is the supported, intended configuration. Warning
// about it would train operators to ignore the line that matters.
func TestDeclaredStandaloneIsNotWarnedAbout(t *testing.T) {
	out := captureBootMode(t, "inmemory", "memory", sharedServices.ModeStandalone)

	if strings.Contains(out, "WARNING") || strings.Contains(out, "SINGLE-INSTANCE ONLY") {
		t.Fatalf("a declared standalone install must not be warned at; got: %q", out)
	}
}

func captureSecretLog(t *testing.T, secret, cacheProvider, lockProvider, declaredMode string) string {
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

	logSigningSecretFingerprint(secret, cacheProvider, lockProvider, declaredMode)
	return buf.String()
}

// The whole point: an operator can only tell whether two instances share a signing secret
// by comparing something, and this is the only something available.
func TestSigningSecretFingerprintIsPrintedWhenMultiInstanceIsPlausible(t *testing.T) {
	const secret = "a-configured-signing-secret"

	for _, tc := range []struct{ name, cache, lock, mode string }{
		{"declared cluster", "inmemory", "memory", sharedServices.ModeClustered},
		{"shared cache", "redis", "memory", ""},
		{"distributed lock", "inmemory", "redis", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := captureSecretLog(t, secret, tc.cache, tc.lock, tc.mode)
			if !strings.Contains(out, "signing secret fingerprint=") {
				t.Fatalf("expected a fingerprint line; got: %q", out)
			}
			// It has to say what to DO with the value, or it is a hex string nobody acts on.
			if !strings.Contains(out, "same value") {
				t.Fatalf("the line must say the value has to match across instances; got: %q", out)
			}
		})
	}
}

// It is a fingerprint precisely so the secret itself never reaches a log file, which is
// copied, shipped to log aggregators and pasted into tickets.
func TestSigningSecretFingerprintNeverLogsTheSecret(t *testing.T) {
	const secret = "super-secret-signing-value"
	out := captureSecretLog(t, secret, "redis", "redis", sharedServices.ModeClustered)

	if strings.Contains(out, secret) {
		t.Fatalf("the signing secret leaked into the log: %q", out)
	}
	if fp := atrest.FingerprintSecret(secret); !strings.Contains(out, fp) {
		t.Fatalf("expected the fingerprint %q in: %q", fp, out)
	}
}

// A genuinely standalone install has nothing to compare against. A line that means nothing
// to most installs is a line everybody learns to skip, which costs the warnings that matter.
func TestSigningSecretFingerprintSilentOnStandalone(t *testing.T) {
	if out := captureSecretLog(t, "a-secret", "inmemory", "memory", ""); out != "" {
		t.Fatalf("undeclared single-instance install should print nothing; got: %q", out)
	}
	if out := captureSecretLog(t, "a-secret", "inmemory", "memory", sharedServices.ModeStandalone); out != "" {
		t.Fatalf("declared standalone install should print nothing; got: %q", out)
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
		if got := sharedServices.IsSharedCacheProvider(provider); got != want {
			t.Fatalf("IsSharedCacheProvider(%q) = %v want %v", provider, got, want)
		}
	}
}
