package coordination

import (
	"context"
	"time"

	"github.com/mysayasan/kopiv2/infra/telemetry"
)

// Locker serializes work for one resource key.
type Locker interface {
	Lock(ctx context.Context, resource string) (Lock, error)
	Ping(ctx context.Context) error
	Close() error
}

// Lock is an acquired resource lock.
type Lock interface {
	Resource() string
	Token() string
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
