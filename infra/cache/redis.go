package cache

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"sort"
	"strings"
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
