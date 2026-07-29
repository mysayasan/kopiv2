package dbsql

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// openIdle gives us a *sql.DB to configure. sql.Open does not dial, so no driver has to be
// reachable — only the pool settings are under test, and Stats() reports them back.
func openIdle(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// The regression that matters: database/sql defaults to UNLIMITED open connections, so an
// engine that simply forgot to configure a pool would let one traffic burst consume the
// whole database server's connection budget — taking down every other application sharing
// it. A zero config must therefore land on the bounded default, not on Go's default.
func TestApplyPoolZeroConfigIsBoundedNotUnlimited(t *testing.T) {
	db := openIdle(t)
	ApplyPool(db, DbPoolConfigModel{})

	if got := db.Stats().MaxOpenConnections; got != defaultMaxOpenConns {
		t.Fatalf("MaxOpenConnections = %d; an unset pool must be bounded at %d, and 0 would mean unlimited", got, defaultMaxOpenConns)
	}
}

// Operator values are honoured as given.
func TestApplyPoolHonoursOperatorValues(t *testing.T) {
	db := openIdle(t)
	ApplyPool(db, DbPoolConfigModel{
		MaxOpenConns:       50,
		MaxIdleConns:       10,
		MaxLifetimeSeconds: 60,
		MaxIdleTimeSeconds: 30,
	})

	if got := db.Stats().MaxOpenConnections; got != 50 {
		t.Fatalf("MaxOpenConnections = %d want 50", got)
	}
}

// Unlimited stays possible, but only when asked for explicitly — the point of the default
// is that nobody arrives at "unlimited" by omission.
func TestApplyPoolUnlimitedIsOptInOnly(t *testing.T) {
	db := openIdle(t)
	ApplyPool(db, DbPoolConfigModel{Unlimited: true})

	if got := db.Stats().MaxOpenConnections; got != 0 {
		t.Fatalf("MaxOpenConnections = %d want 0 (unlimited) when explicitly requested", got)
	}
}

// More idle than open is not an error to database/sql — it silently reduces the idle count
// — so an operator who wrote maxIdleConns:100 with maxOpenConns:10 would get 10 while the
// config claims 100. ApplyPool clamps it so the two at least agree.
//
// database/sql exposes MaxOpenConnections but no MaxIdleConns getter, so this asserts the
// clamp through the resolver rather than through Stats(). That is a narrower claim than
// "the pool behaves correctly", and saying so is better than a test that reads stronger
// than it is: the open-connection bound above is the one with real teeth.
func TestApplyPoolClampsIdleAboveOpen(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  DbPoolConfigModel
		want int
	}{
		{"explicit open bounds idle", DbPoolConfigModel{MaxOpenConns: 10, MaxIdleConns: 100}, 10},
		{"default open bounds idle", DbPoolConfigModel{MaxIdleConns: 100}, defaultMaxOpenConns},
		{"idle below open is untouched", DbPoolConfigModel{MaxOpenConns: 50, MaxIdleConns: 7}, 7},
		{"unlimited leaves idle alone", DbPoolConfigModel{Unlimited: true, MaxIdleConns: 100}, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveMaxIdleConns(tc.cfg); got != tc.want {
				t.Fatalf("resolveMaxIdleConns(%+v) = %d want %d", tc.cfg, got, tc.want)
			}
			// And the configuration still applies cleanly to a real handle.
			ApplyPool(openIdle(t), tc.cfg)
		})
	}
}

// A guard on the DEFAULTS themselves, not on ApplyPool — database/sql exposes no
// ConnMaxLifetime getter, so the applied value cannot be read back, and a test that
// pretended otherwise would be theatre.
//
// It still earns its place: a connection held open indefinitely outlives failovers, DNS
// changes and rotated credentials, and some poolers silently drop long-idle connections,
// which surfaces much later as a random "bad connection". Someone editing these constants
// to 0 would disable that protection with nothing else complaining.
func TestConnectionLifetimeDefaultsAreSane(t *testing.T) {
	if defaultConnMaxLifetime <= 0 || defaultConnMaxIdleTime <= 0 {
		t.Fatal("a non-positive default means database/sql never retires a connection, so failovers and rotated credentials go unnoticed")
	}
	if defaultConnMaxLifetime > 24*time.Hour {
		t.Fatalf("a %v lifetime is long enough to survive a failover it should not", defaultConnMaxLifetime)
	}
	if defaultConnMaxIdleTime > defaultConnMaxLifetime {
		t.Fatalf("idle timeout %v exceeds the lifetime %v, so it can never take effect", defaultConnMaxIdleTime, defaultConnMaxLifetime)
	}
	if defaultMaxIdleConns > defaultMaxOpenConns {
		t.Fatalf("default idle %d exceeds default open %d", defaultMaxIdleConns, defaultMaxOpenConns)
	}
}

// A nil handle must not panic: ApplyPool is called on a path where the engine constructor
// may have failed, and a panic there would take the whole app down over a pool setting.
func TestApplyPoolNilDbIsSafe(t *testing.T) {
	ApplyPool(nil, DbPoolConfigModel{MaxOpenConns: 5})
}
