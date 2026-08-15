package coordination

import (
	"context"
	"errors"
	"time"

	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// ErrNotAcquired is returned by TryLock when the resource is already held.
// It is an ordinary outcome, not a failure: the caller was asking whether it is
// the one to do the work, and the answer was no.
var ErrNotAcquired = errors.New("coordination: resource is already locked")

// Locker serializes work for one resource key.
type Locker interface {
	// Lock waits its turn in a FIFO queue and returns once the resource is held,
	// or fails after WaitTimeout. Use it for work that MUST eventually happen.
	Lock(ctx context.Context, resource string) (Lock, error)
	// TryLock attempts the resource once and returns ErrNotAcquired immediately if
	// somebody else holds it. Use it for work that must happen at most ONCE across
	// the deployment, where a loser should skip rather than wait.
	//
	// Lock is deliberately unsuitable there: it queues, so a loser would block for
	// WaitTimeout and then acquire the moment the winner released — running exactly
	// the job the lock was meant to deduplicate, only later.
	TryLock(ctx context.Context, resource string) (Lock, error)
	Ping(ctx context.Context) error
	Close() error
}

// Lock is an acquired resource lock.
type Lock interface {
	Resource() string
	Token() string
	// Valid reports whether this lock is still held by this holder. A lease can be
	// lost without anybody being told — the process stalls past the TTL, or the
	// backing store drops the key — and a holder that assumes otherwise becomes a
	// second writer. Long-lived holders should re-check rather than assume.
	Valid(ctx context.Context) bool
	Release(ctx context.Context) error
}

// Config controls lock timing and metric labels.
type Config struct {
	AppName        string
	Provider       string
	KeyPrefix      string
	WaitTimeout    time.Duration
	LeaseTTL       time.Duration
	StuckTimeout   time.Duration
	PollInterval   time.Duration
	RenewInterval  time.Duration
	RedisAddress   string
	RedisPassword  string
	RedisDB        int
	RedisUseTLS    bool
	ConnectTimeout time.Duration
	CommandTimeout time.Duration
}

type recorder interface {
	ObserveCoordination(metric telemetry.CoordinationMetric)
}

func normalizeConfig(cfg Config) Config {
	if cfg.WaitTimeout <= 0 {
		cfg.WaitTimeout = 30 * time.Second
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 10 * time.Second
	}
	if cfg.StuckTimeout <= 0 {
		cfg.StuckTimeout = 30 * time.Second
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 100 * time.Millisecond
	}
	if cfg.RenewInterval <= 0 {
		cfg.RenewInterval = cfg.LeaseTTL / 3
		if cfg.RenewInterval <= 0 {
			cfg.RenewInterval = time.Second
		}
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 2 * time.Second
	}
	if cfg.CommandTimeout <= 0 {
		cfg.CommandTimeout = 2 * time.Second
	}
	return cfg
}

func observe(rec recorder, metric telemetry.CoordinationMetric) {
	if rec != nil {
		rec.ObserveCoordination(metric)
	}
}
