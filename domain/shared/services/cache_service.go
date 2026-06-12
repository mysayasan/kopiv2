package services

import (
	"context"

	"github.com/mysayasan/kopiv2/infra/cache"
)

// cacheService struct
type cacheService struct {
	cacheStore cache.Store
}

// Create new ICacheService
func NewCacheService(cacheStore cache.Store) ICacheService {
	return &cacheService{cacheStore: cacheStore}
}

func (m *cacheService) ListKeys(ctx context.Context, prefix string, limit uint64, offset uint64) ([]string, uint64, error) {
	return m.cacheStore.ListKeys(ctx, prefix, limit, offset)
}

func (m *cacheService) WipeByPrefix(ctx context.Context, prefix string) (bool, error) {
	if err := m.cacheStore.DeleteByPrefix(ctx, prefix); err != nil {
		return false, err
	}
	return true, nil
}

func (m *cacheService) WipeByKey(ctx context.Context, key string) (bool, error) {
	if err := m.cacheStore.Delete(ctx, key); err != nil {
		return false, err
	}
	return true, nil
}

func (m *cacheService) Ping(ctx context.Context) (bool, error) {
	if err := m.cacheStore.Ping(ctx); err != nil {
		return false, err
	}
	return true, nil
}
