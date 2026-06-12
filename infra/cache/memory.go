package cache

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	goCache "github.com/patrickmn/go-cache"
)

// MemoryStore uses process-local go-cache and is useful for local fallback/testing.
type MemoryStore struct {
	cache       *goCache.Cache
	rateMu      sync.Mutex
	rateWindows map[string][]int64
}

func NewMemoryStore(defaultTTL time.Duration, cleanupInterval time.Duration) *MemoryStore {
	if defaultTTL <= 0 {
		defaultTTL = 10 * time.Second
	}
	if cleanupInterval <= 0 {
		cleanupInterval = defaultTTL
	}

	return &MemoryStore{
		cache:       goCache.New(defaultTTL, cleanupInterval),
		rateWindows: make(map[string][]int64),
	}
}

func (m *MemoryStore) Get(_ context.Context, key string, dest any) (bool, error) {
	v, found := m.cache.Get(key)
	if !found {
		return false, nil
	}

	switch raw := v.(type) {
	case []byte:
		if err := json.Unmarshal(raw, dest); err != nil {
			return false, err
		}
		return true, nil
	default:
		b, err := json.Marshal(raw)
		if err != nil {
			return false, err
		}
		if err := json.Unmarshal(b, dest); err != nil {
			return false, err
		}
		return true, nil
	}
}

func (m *MemoryStore) Set(_ context.Context, key string, value any, ttl time.Duration) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m.cache.Set(key, b, ttl)
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, key string) error {
	m.cache.Delete(key)
	return nil
}

func (m *MemoryStore) DeleteByPrefix(_ context.Context, prefix string) error {
	for k := range m.cache.Items() {
		if strings.HasPrefix(k, prefix) {
			m.cache.Delete(k)
		}
	}
	return nil
}

func (m *MemoryStore) ListKeys(_ context.Context, prefix string, limit uint64, offset uint64) ([]string, uint64, error) {
	keys := make([]string, 0)
	for k := range m.cache.Items() {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}

	sort.Strings(keys)
	total := uint64(len(keys))

	if offset >= total {
		return []string{}, total, nil
	}

	start := offset
	end := total
	if limit > 0 {
		candidate := offset + limit
		if candidate < end {
			end = candidate
		}
	}

	return keys[start:end], total, nil
}

func (m *MemoryStore) AllowSlidingWindow(_ context.Context, key string, limit int64, window time.Duration, now time.Time) (SlidingWindowResult, error) {
	if limit <= 0 || window <= 0 {
		return SlidingWindowResult{Allowed: true, Limit: limit, Remaining: limit}, nil
	}

	nowMs := now.UnixMilli()
	cutoffMs := now.Add(-window).UnixMilli()

	m.rateMu.Lock()
	defer m.rateMu.Unlock()

	rawWindow := m.rateWindows[key]
	active := rawWindow[:0]
	for _, ts := range rawWindow {
		if ts > cutoffMs {
			active = append(active, ts)
		}
	}

	count := int64(len(active))
	allowed := count < limit
	if allowed {
		active = append(active, nowMs)
		count++
	}

	if len(active) == 0 {
		delete(m.rateWindows, key)
	} else {
		m.rateWindows[key] = active
	}

	retryAfter := time.Duration(0)
	resetAfter := window
	if len(active) > 0 {
		resetAt := time.UnixMilli(active[0]).Add(window)
		resetAfter = resetAt.Sub(now)
		if resetAfter < 0 {
			resetAfter = 0
		}
		if !allowed {
			retryAfter = resetAfter
		}
	}

	remaining := limit - count
	if remaining < 0 {
		remaining = 0
	}

	return SlidingWindowResult{
		Allowed:    allowed,
		Limit:      limit,
		Count:      count,
		Remaining:  remaining,
		RetryAfter: retryAfter,
		ResetAfter: resetAfter,
	}, nil
}

func (m *MemoryStore) Ping(_ context.Context) error {
	return nil
}

func (m *MemoryStore) Close() error {
	return nil
}
