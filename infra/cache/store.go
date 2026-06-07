package cache

import (
	"context"
	"time"
)

// Store defines shared cache behavior across cache providers.
type Store interface {
	Get(ctx context.Context, key string, dest any) (bool, error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	DeleteByPrefix(ctx context.Context, prefix string) error
	ListKeys(ctx context.Context, prefix string, limit uint64, offset uint64) ([]string, uint64, error)
	Ping(ctx context.Context) error
	Close() error
}
