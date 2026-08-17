package apphost

import (
	"context"
	"errors"
	"testing"

	"github.com/mysayasan/kopiv2/infra/config"
)

func lockTestConfig(lockProvider, cacheProvider string) *config.AppConfigModel {
	cfg := &config.AppConfigModel{}
	cfg.Transaction.LockProvider = lockProvider
	cfg.Cache.Provider = cacheProvider
	return cfg
}

// The overwhelming majority of installs are single-instance. They must not pay for,
// or be able to be blocked by, a lock nothing else can see.
func TestMigrationLockSkippedForPerProcessProvider(t *testing.T) {
	for _, tc := range []struct{ name, lock, cache string }{
		{"unset falls back to cache", "", "default"},
		{"explicit memory", "memory", "default"},
		{"inmemory cache", "", "inmemory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ran := false
			err := withMigrationLock(context.Background(), "test", lockTestConfig(tc.lock, tc.cache), func() error {
				ran = true
				return nil
			})
			if err != nil {
				t.Fatalf("withMigrationLock: %v", err)
			}
			if !ran {
				t.Fatal("bootstrap must run even when no lock is taken")
			}
		})
	}
}

// The bootstrap error has to reach the caller unchanged: a failed migration must stop
// the boot, not be swallowed by the thing that was only supposed to serialize it.
func TestMigrationLockPropagatesBootstrapError(t *testing.T) {
	sentinel := errors.New("schema is broken")
	err := withMigrationLock(context.Background(), "test", lockTestConfig("memory", ""), func() error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want the bootstrap error propagated", err)
	}
}

// An unreachable Redis must not stop the app booting. Refusing to start because a
// coordination store blinked would turn an availability feature into an outage; the
// risk it guards (two instances migrating at the same instant) is far rarer.
func TestMigrationLockProceedsWhenLockUnavailable(t *testing.T) {
	cfg := lockTestConfig("redis", "")
	// Nothing is listening here, so acquiring must fail.
	cfg.Cache.Redis.Address = "127.0.0.1:1"
	cfg.Cache.Redis.ConnectTimeoutMs = 50
	cfg.Cache.Redis.OperationTimeoutMs = 50
	cfg.Transaction.LockWaitTimeoutMs = 100

	ran := false
	err := withMigrationLock(context.Background(), "test", cfg, func() error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("an unreachable lock store must not fail the boot: %v", err)
	}
	if !ran {
		t.Fatal("bootstrap must still run when the lock cannot be acquired")
	}
}

// Exclusion itself is covered where it is implemented (infra/coordination's lock
// tests) and end-to-end by the two-instance bench. Asserting it here with a
// per-process provider would prove nothing, because that provider takes no lock.
