package cache

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisConfig defines redis connection options for shared cache.
type RedisConfig struct {
	Address          string
	Password         string
	DB               int
	UseTLS           bool
	KeyPrefix        string
	ConnectTimeout   time.Duration
	OperationTimeout time.Duration
}

// RedisStore uses Redis as shared cache across app instances.
type RedisStore struct {
	client           *redis.Client
	keyPrefix        string
	operationTimeout time.Duration
}

func NewRedisStore(cfg RedisConfig) *RedisStore {
	tlsConfig := (*tls.Config)(nil)
	if cfg.UseTLS {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Address,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.ConnectTimeout,
		ReadTimeout:  cfg.OperationTimeout,
		WriteTimeout: cfg.OperationTimeout,
		TLSConfig:    tlsConfig,
	})

	if cfg.OperationTimeout <= 0 {
		cfg.OperationTimeout = 2 * time.Second
	}

	return &RedisStore{
		client:           client,
		keyPrefix:        strings.TrimSpace(cfg.KeyPrefix),
		operationTimeout: cfg.OperationTimeout,
	}
}

func (r *RedisStore) Get(ctx context.Context, key string, dest any) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	res, err := r.client.Get(ctx, r.prefixedKey(key)).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(res, dest); err != nil {
		return false, err
	}

	return true, nil
}

func (r *RedisStore) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	b, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return r.client.Set(ctx, r.prefixedKey(key), b, ttl).Err()
}

func (r *RedisStore) Delete(ctx context.Context, key string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.client.Del(ctx, r.prefixedKey(key)).Err()
}

func (r *RedisStore) DeleteByPrefix(ctx context.Context, prefix string) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	pattern := r.prefixedKey(prefix) + "*"
	var cursor uint64
	for {
		keys, nextCursor, err := r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}

		if len(keys) > 0 {
			if err := r.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return nil
}

func (r *RedisStore) ListKeys(ctx context.Context, prefix string, limit uint64, offset uint64) ([]string, uint64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	pattern := r.prefixedKey(prefix) + "*"
	keys := make([]string, 0)
	var cursor uint64
	for {
		rawKeys, nextCursor, err := r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, 0, err
		}

		for _, k := range rawKeys {
			keys = append(keys, r.unprefixKey(k))
		}

		cursor = nextCursor
		if cursor == 0 {
			break
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

func (r *RedisStore) AllowSlidingWindow(ctx context.Context, key string, limit int64, window time.Duration, now time.Time) (SlidingWindowResult, error) {
	if limit <= 0 || window <= 0 {
		return SlidingWindowResult{Allowed: true, Limit: limit, Remaining: limit}, nil
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	windowMs := window.Milliseconds()
	nowMs := now.UnixMilli()
	cutoffMs := now.Add(-window).UnixMilli()
	member := strconv.FormatInt(now.UnixNano(), 10) + "-" + strconv.FormatUint(atomic.AddUint64(&redisSlidingWindowNonce, 1), 10)

	values, err := redisSlidingWindowScript.Run(
		ctx,
		r.client,
		[]string{r.prefixedKey(key)},
		strconv.FormatInt(nowMs, 10),
		strconv.FormatInt(cutoffMs, 10),
		strconv.FormatInt(windowMs, 10),
		strconv.FormatInt(limit, 10),
		member,
	).Slice()
	if err != nil {
		return SlidingWindowResult{}, err
	}
	if len(values) != 4 {
		return SlidingWindowResult{}, redis.Nil
	}

	allowed := toInt64(values[0]) == 1
	count := toInt64(values[1])
	retryAfterMs := toInt64(values[2])
	resetAfterMs := toInt64(values[3])
	remaining := limit - count
	if remaining < 0 {
		remaining = 0
	}

	return SlidingWindowResult{
		Allowed:    allowed,
		Limit:      limit,
		Count:      count,
		Remaining:  remaining,
		RetryAfter: time.Duration(retryAfterMs) * time.Millisecond,
		ResetAfter: time.Duration(resetAfterMs) * time.Millisecond,
	}, nil
}

func (r *RedisStore) Ping(ctx context.Context) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.client.Ping(ctx).Err()
}

func (r *RedisStore) Close() error {
	return r.client.Close()
}

func (r *RedisStore) prefixedKey(key string) string {
	if r.keyPrefix == "" {
		return key
	}
	return r.keyPrefix + ":" + key
}

func (r *RedisStore) unprefixKey(key string) string {
	if r.keyPrefix == "" {
		return key
	}

	prefix := r.keyPrefix + ":"
	if strings.HasPrefix(key, prefix) {
		return strings.TrimPrefix(key, prefix)
	}

	return key
}

func (r *RedisStore) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.operationTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, r.operationTimeout)
}

var redisSlidingWindowScript = redis.NewScript(`
local now = tonumber(ARGV[1])
local cutoff = tonumber(ARGV[2])
local window = tonumber(ARGV[3])
local limit = tonumber(ARGV[4])
local member = ARGV[5]

redis.call("ZREMRANGEBYSCORE", KEYS[1], 0, cutoff)

local count = redis.call("ZCARD", KEYS[1])
if count >= limit then
	local oldest = redis.call("ZRANGE", KEYS[1], 0, 0, "WITHSCORES")
	local retry_after = window
	if oldest[2] ~= nil then
		retry_after = math.max(0, tonumber(oldest[2]) + window - now)
	end
	redis.call("PEXPIRE", KEYS[1], window)
	return {0, count, retry_after, retry_after}
end

redis.call("ZADD", KEYS[1], now, member)
count = count + 1
redis.call("PEXPIRE", KEYS[1], window)

local oldest = redis.call("ZRANGE", KEYS[1], 0, 0, "WITHSCORES")
local reset_after = window
if oldest[2] ~= nil then
	reset_after = math.max(0, tonumber(oldest[2]) + window - now)
end

return {1, count, 0, reset_after}
`)

var redisSlidingWindowNonce uint64

func toInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
}
