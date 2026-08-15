package services

import (
	"context"
	"errors"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
)

// clusterReadyEnv is a deployment that should pass every blocker: a client/server
// database, a shared cache and lock that both answer, an at-rest key, and an
// explicitly configured signing secret. Each test starts from this and breaks
// exactly one thing, so a failure names the check that regressed.
func clusterReadyEnv() DeploymentEnv {
	return DeploymentEnv{
		DbEngine:          "postgres",
		CacheProvider:     "redis",
		LockProvider:      "redis",
		JwtSecret:         "not-empty",
		MaxOpenConns:      25,
		AtRestEnabled:     true,
		AtRestFingerprint: "a1b2c3d4e5f60718",
		CachePing:         func(context.Context) error { return nil },
		LockPing:          func(context.Context) error { return nil },
	}
}

// checkById returns the named row, failing the test when it is absent — a check
// that silently disappears would otherwise read as a pass.
func checkById(t *testing.T, report PreflightReport, id string) PreflightCheck {
	t.Helper()
	for _, c := range report.Checks {
		if c.Id == id {
			return c
		}
	}
	t.Fatalf("preflight is missing check %q", id)
	return PreflightCheck{}
}

func TestDeploymentModeLifecycle(t *testing.T) {
	repo := &fakeSetupSettingRepo{}
	svc := NewDeploymentModeService(repo)
	ctx := context.Background()

	// An install that never answered the question is standalone, not blank —
	// every install behaved that way before this existed.
	state, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if state.Mode != ModeStandalone || state.Clustered() {
		t.Fatalf("fresh install: got %+v, want standalone", state)
	}

	if _, err := svc.Set(ctx, ModeClustered, true); err != nil {
		t.Fatalf("Set: %v", err)
	}
	state, err = svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if !state.Clustered() || !state.Acknowledged {
		t.Fatalf("after Set: got %+v, want clustered + acknowledged", state)
	}
	if state.UpdatedAt == 0 {
		t.Fatal("Set must stamp UpdatedAt")
	}

	// Switching back must clear the acknowledgment: it was an acceptance of the
	// clustered caveats, and leaving it set would later read as "already agreed".
	if _, err := svc.Set(ctx, ModeStandalone, false); err != nil {
		t.Fatalf("Set standalone: %v", err)
	}
	state, _ = svc.Get(ctx)
	if state.Clustered() || state.Acknowledged {
		t.Fatalf("after revert: got %+v, want standalone + unacknowledged", state)
	}

	// Exactly one row, updated in place rather than appended.
	if len(repo.rows) != 1 {
		t.Fatalf("got %d rows, want 1 (Set must update in place)", len(repo.rows))
	}
}

func TestDeploymentModeRejectsUnknownValue(t *testing.T) {
	svc := NewDeploymentModeService(&fakeSetupSettingRepo{})
	if _, err := svc.Set(context.Background(), "ha", false); err == nil {
		t.Fatal("an unrecognised mode must be rejected, not stored")
	}
}

// A repo that signals "missing" with a zero row rather than an error would
// otherwise UPDATE id 0 and persist nothing at all.
func TestDeploymentModeHandlesZeroRowMiss(t *testing.T) {
	repo := &fakeSetupSettingRepo{missAsZeroRow: true}
	svc := NewDeploymentModeService(repo)
	ctx := context.Background()

	if _, err := svc.Set(ctx, ModeClustered, true); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(repo.rows) != 1 {
		t.Fatalf("got %d rows, want 1 — the zero-row miss must INSERT", len(repo.rows))
	}
	if state, _ := svc.Get(ctx); !state.Clustered() {
		t.Fatalf("got %+v, want clustered", state)
	}
}

// A corrupt value must read as standalone. Standalone is the safe direction to
// fail: it keeps every singleton running on this instance, which is what a lone
// process needs — failing the other way would silently stop them everywhere.
func TestDeploymentModeCorruptValueReadsStandalone(t *testing.T) {
	repo := &fakeSetupSettingRepo{rows: []*entities.RuntimeSetting{
		{Id: 1, Key: DeploymentModeKey, Value: "{not json"},
	}}
	state, err := NewDeploymentModeService(repo).Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if state.Mode != ModeStandalone {
		t.Fatalf("got %q, want standalone", state.Mode)
	}
}

func TestPreflightApplianceShortCircuits(t *testing.T) {
	env := clusterReadyEnv()
	env.Appliance = true
	env.ApplianceReason = ApplianceSerialBus

	report := Preflight(context.Background(), env, DeploymentState{Mode: ModeClustered})
	if report.Clusterable {
		t.Fatal("an appliance must never report as clusterable")
	}
	if report.ApplianceReason == "" {
		t.Fatal("an appliance must explain why")
	}
	if len(report.Checks) != 0 {
		t.Fatalf("got %d checks, want none — a checklist implies the answer is yes", len(report.Checks))
	}
	// Even asked about a clustered deployment, the reported mode is the truth.
	if report.Mode != ModeStandalone {
		t.Fatalf("got mode %q, want standalone", report.Mode)
	}
}

func TestPreflightReadyWhenFullyConfigured(t *testing.T) {
	report := Preflight(context.Background(), clusterReadyEnv(), DeploymentState{Mode: ModeClustered})
	if !report.Clusterable || !report.Ready {
		t.Fatalf("got clusterable=%v ready=%v, want both true", report.Clusterable, report.Ready)
	}
	// The key id is the entire value of that row — an operator compares it between
	// instances, so it has to reach the payload.
	if got := checkById(t, report, CheckAtRestKey); got.Detail != "a1b2c3d4e5f60718" {
		t.Fatalf("at-rest key id: got %q, want it surfaced for comparison", got.Detail)
	}
}

func TestPreflightBlockers(t *testing.T) {
	cases := []struct {
		name   string
		check  string
		break_ func(*DeploymentEnv)
	}{
		{"sqlite cannot be clustered", CheckDbEngine, func(e *DeploymentEnv) { e.DbEngine = "sqlite" }},
		{"per-process cache", CheckSharedCache, func(e *DeploymentEnv) { e.CacheProvider = "default"; e.CachePing = nil }},
		{"per-process lock", CheckSharedLock, func(e *DeploymentEnv) { e.LockProvider = "memory"; e.LockPing = nil }},
		{"redis named but unreachable", CheckSharedCache, func(e *DeploymentEnv) {
			e.CachePing = func(context.Context) error { return errors.New("dial tcp: connection refused") }
		}},
		{"lock named but unreachable", CheckSharedLock, func(e *DeploymentEnv) {
			e.LockPing = func(context.Context) error { return errors.New("dial tcp: connection refused") }
		}},
		{"unset jwt secret", CheckJwtSecret, func(e *DeploymentEnv) { e.JwtSecret = "  " }},
		// The one that actually happens: the host generated a secret because none was
		// configured, and wrote it back — so the config now LOOKS configured while every
		// other replica holds a different key.
		{"self-generated jwt secret", CheckJwtSecret, func(e *DeploymentEnv) { e.JwtSecretGenerated = true }},
		{"encryption on with no key", CheckAtRestKey, func(e *DeploymentEnv) { e.AtRestFingerprint = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := clusterReadyEnv()
			tc.break_(&env)
			report := Preflight(context.Background(), env, DeploymentState{Mode: ModeClustered})
			if report.Ready {
				t.Fatal("a failed blocker must clear Ready")
			}
			got := checkById(t, report, tc.check)
			if got.Ok {
				t.Fatalf("check %q should have failed", tc.check)
			}
			if got.Severity != SeverityBlocker {
				t.Fatalf("check %q: got severity %q, want blocker", tc.check, got.Severity)
			}
		})
	}
}

// Encryption off means there is nothing sealed for two instances to disagree
// about, so the row passes rather than demanding a key that does not exist.
func TestPreflightAtRestDisabledPasses(t *testing.T) {
	env := clusterReadyEnv()
	env.AtRestEnabled = false
	env.AtRestFingerprint = ""

	report := Preflight(context.Background(), env, DeploymentState{Mode: ModeClustered})
	if !report.Ready {
		t.Fatal("encryption-at-rest off must not block clustering")
	}
	if got := checkById(t, report, CheckAtRestKey); !got.Ok {
		t.Fatalf("at-rest row should pass when encryption is disabled, got %+v", got)
	}
}

// A deployment that merely wastes resources still works, so warnings must not
// clear Ready — otherwise operators learn to ignore the verdict.
func TestPreflightWarningsDoNotClearReady(t *testing.T) {
	env := clusterReadyEnv()
	env.ExtraChecks = func(context.Context) []PreflightCheck {
		return []PreflightCheck{{Id: "llmMode", Severity: SeverityWarning, Ok: false, Detail: "sidecar"}}
	}
	report := Preflight(context.Background(), env, DeploymentState{Mode: ModeClustered})
	if !report.Ready {
		t.Fatal("a failed WARNING must not clear Ready")
	}
	if got := checkById(t, report, "llmMode"); got.Ok {
		t.Fatal("the app-specific row should still be reported as failed")
	}
}

// The pool row reports the per-instance budget so an operator can multiply it by
// the replica count against their database server's limit.
func TestPreflightPoolReportsPerInstanceBudget(t *testing.T) {
	env := clusterReadyEnv()
	env.MaxOpenConns = 0 // absent means the 25-connection default

	got := checkById(t, Preflight(context.Background(), env, DeploymentState{Mode: ModeClustered}), CheckDbPool)
	if got.Severity != SeverityWarning {
		t.Fatalf("got severity %q, want warning", got.Severity)
	}
	if got.Detail != "25 per instance" {
		t.Fatalf("got %q, want the resolved default surfaced", got.Detail)
	}
}

func TestSharedProviderPredicates(t *testing.T) {
	for _, p := range []string{"redis", "Redis", " redis-cluster ", "rediscluster"} {
		if !IsSharedCacheProvider(p) {
			t.Errorf("IsSharedCacheProvider(%q) = false, want true", p)
		}
		if !IsDistributedLockProvider(p) {
			t.Errorf("IsDistributedLockProvider(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"", "default", "memory"} {
		if IsSharedCacheProvider(p) {
			t.Errorf("IsSharedCacheProvider(%q) = true, want false", p)
		}
		if IsDistributedLockProvider(p) {
			t.Errorf("IsDistributedLockProvider(%q) = true, want false", p)
		}
	}
}
