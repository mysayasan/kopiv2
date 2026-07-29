package dbsql

import (
	"database/sql"
	"time"
)

// Connection-pool defaults for the client/server engines (Postgres, MariaDB).
//
// The defaults matter more than the knobs. Go's database/sql defaults to UNLIMITED open
// connections, so before this existed a burst of traffic could open as many connections as
// there were concurrent queries and exhaust the customer's server-wide connection budget —
// taking down every other application on that database, not just this one. Postgres ships
// with max_connections=100 shared across everything, so 25 is a considerate slice that
// still leaves headroom, and it is what a well-behaved service is expected to cap itself at.
//
// ConnMaxLifetime exists for a different reason: a connection pinned open for days survives
// failovers and DNS changes it should not, and some proxies (pgbouncer, ProxySQL) silently
// drop long-idle connections, which surfaces as a random "bad connection" much later.
// Recycling on a timer keeps the pool honest.
const (
	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 30 * time.Minute
	defaultConnMaxIdleTime = 5 * time.Minute
)

// DbConfigModel
type DbConfigModel struct {
	Engine   string `json:"engine"`
	Host     string `json:"host" validate:"required"`
	Port     int    `json:"port" validate:"required"`
	User     string `json:"user" validate:"required"`
	Password string `json:"password" validate:"required"`
	DbName   string `json:"db_name" validate:"required"`
	SslMode  string `json:"ssl_mode" validate:"required"`

	// Pool is the connection-pool budget for the client/server engines. Every field is
	// optional; absent or zero means "use the default". SQLite ignores this entirely —
	// it is pinned to a single connection because concurrent writers to one file
	// deadlock, and that is a correctness constraint rather than a tunable.
	Pool DbPoolConfigModel `json:"pool"`
}

// DbPoolConfigModel bounds how many connections this process will hold open.
type DbPoolConfigModel struct {
	// MaxOpenConns caps total connections (in use + idle). Set it below the share of
	// the database server's limit this application is entitled to.
	MaxOpenConns int `json:"maxOpenConns"`
	// MaxIdleConns caps connections kept open when idle. Larger means fewer handshakes
	// under bursty load; smaller frees server resources sooner.
	MaxIdleConns int `json:"maxIdleConns"`
	// MaxLifetimeSeconds retires a connection this long after it was created, however
	// healthy it looks, so failovers and rotated credentials take effect.
	MaxLifetimeSeconds int `json:"maxLifetimeSeconds"`
	// MaxIdleTimeSeconds closes a connection idle for this long.
	MaxIdleTimeSeconds int `json:"maxIdleTimeSeconds"`
	// Unlimited is the deliberate escape hatch for an operator who genuinely wants Go's
	// old unbounded behaviour (a dedicated database server sized for it). It has to be
	// explicit, because arriving at "unlimited" by accident is what this guards against.
	Unlimited bool `json:"unlimited"`
}

// ApplyPool configures a pool on an open *sql.DB. Safe to call with a zero config: every
// unset value falls back to the default rather than to Go's unlimited behaviour, so an
// engine that forgets to pass a config still gets a bounded pool.
func ApplyPool(db *sql.DB, cfg DbPoolConfigModel) {
	if db == nil {
		return
	}
	if cfg.Unlimited {
		// 0 means unlimited to database/sql. Idle and lifetime bounds still apply, so
		// even here connections do not live forever.
		db.SetMaxOpenConns(0)
	} else {
		maxOpen := cfg.MaxOpenConns
		if maxOpen <= 0 {
			maxOpen = defaultMaxOpenConns
		}
		db.SetMaxOpenConns(maxOpen)
	}

	db.SetMaxIdleConns(resolveMaxIdleConns(cfg))

	lifetime := time.Duration(cfg.MaxLifetimeSeconds) * time.Second
	if lifetime <= 0 {
		lifetime = defaultConnMaxLifetime
	}
	db.SetConnMaxLifetime(lifetime)

	idleTime := time.Duration(cfg.MaxIdleTimeSeconds) * time.Second
	if idleTime <= 0 {
		idleTime = defaultConnMaxIdleTime
	}
	db.SetConnMaxIdleTime(idleTime)
}

// resolveMaxIdleConns picks the idle bound, never letting it exceed the open bound.
//
// More idle than open is not an error to database/sql — it silently reduces the idle count
// to match — but that leaves the config claiming a number the pool never honours. Split out
// from ApplyPool because database/sql exposes no MaxIdleConns getter, so this is the only
// way to assert the rule directly rather than restating it in a test.
func resolveMaxIdleConns(cfg DbPoolConfigModel) int {
	maxIdle := cfg.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = defaultMaxIdleConns
	}
	if cfg.Unlimited {
		// No open bound to clamp against.
		return maxIdle
	}
	open := cfg.MaxOpenConns
	if open <= 0 {
		open = defaultMaxOpenConns
	}
	if maxIdle > open {
		return open
	}
	return maxIdle
}
