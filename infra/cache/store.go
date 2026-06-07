package cache

import (
	"context"
	"time"
)

// SlidingWindowResult describes one sliding-window rate-limit decision.
type SlidingWindowResult struct {
	Allowed    bool
	Limit      int64
	Count      int64
	Remaining  int64
	RetryAfter time.Duration
	ResetAfter time.Duration
}

// Store defines shared cache behavior across cache providers.
type Store interface {
	Get(ctx context.Context, key string, dest any) (bool, error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	DeleteByPrefix(ctx context.Context, prefix string) error
	ListKeys(ctx context.Context, prefix string, limit uint64, offset uint64) ([]string, uint64, error)
	AllowSlidingWindow(ctx context.Context, key string, limit int64, window time.Duration, now time.Time) (SlidingWindowResult, error)
	Ping(ctx context.Context) error
	Close() error
}
