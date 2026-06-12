package coordination

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mysayasan/kopiv2/infra/telemetry"
	"github.com/redis/go-redis/v9"
)

type RedisLocker struct {
	client   *redis.Client
	cfg      Config
	recorder recorder
}

type redisLock struct {
	parent   *RedisLocker
	resource string
	token    string
	done     chan struct{}
	once     sync.Once
}

func NewRedisLocker(cfg Config, rec recorder) *RedisLocker {
	cfg = normalizeConfig(cfg)
	if cfg.Provider == "" {
		cfg.Provider = "redis"
	}

	tlsConfig := (*tls.Config)(nil)
	if cfg.RedisUseTLS {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	return &RedisLocker{
		client: redis.NewClient(&redis.Options{
			Addr:         cfg.RedisAddress,
			Password:     cfg.RedisPassword,
			DB:           cfg.RedisDB,
			DialTimeout:  cfg.ConnectTimeout,
			ReadTimeout:  cfg.CommandTimeout,
			WriteTimeout: cfg.CommandTimeout,
			TLSConfig:    tlsConfig,
		}),
		cfg:      cfg,
		recorder: rec,
	}
}

func (r *RedisLocker) Lock(ctx context.Context, resource string) (Lock, error) {
	start := time.Now()
	token := uuid.NewString()
	queueKey := r.queueKey(resource)
	waitKey := r.waitKey(resource, token)
	waitCtx, cancel := context.WithTimeout(ctx, r.cfg.WaitTimeout)
	defer cancel()

	if err := r.client.RPush(waitCtx, queueKey, token).Err(); err != nil {
		r.record(resource, "error", start)
		return nil, err
	}
	if err := r.client.Set(waitCtx, waitKey, "1", r.cfg.WaitTimeout+r.cfg.LeaseTTL).Err(); err != nil {
		_ = r.client.LRem(context.Background(), queueKey, 0, token).Err()
		r.record(resource, "error", start)
		return nil, err
	}

	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		if err := r.client.Expire(waitCtx, waitKey, r.cfg.WaitTimeout+r.cfg.LeaseTTL).Err(); err != nil {
			_ = r.removeWaiter(context.Background(), resource, token)
			r.record(resource, "error", start)
			return nil, err
		}

		head, err := r.client.LIndex(waitCtx, queueKey, 0).Result()
		if err != nil && err != redis.Nil {
			_ = r.removeWaiter(context.Background(), resource, token)
			r.record(resource, "error", start)
			return nil, err
		}

		if head == token {
			acquired, err := r.client.SetNX(waitCtx, r.lockKey(resource), token, r.cfg.LeaseTTL).Result()
			if err != nil {
				_ = r.removeWaiter(context.Background(), resource, token)
				r.record(resource, "error", start)
				return nil, err
			}
			if acquired {
				_, _ = r.client.LPop(waitCtx, queueKey).Result()
				_ = r.client.Del(waitCtx, waitKey).Err()
				lock := &redisLock{
					parent:   r,
					resource: resource,
					token:    token,
					done:     make(chan struct{}),
				}
				lock.startRenewal()
				lock.monitor()
				r.record(resource, "acquired", start)
				return lock, nil
			}
		} else if head != "" {
			if err := r.dropStaleHead(waitCtx, resource, head); err != nil {
				_ = r.removeWaiter(context.Background(), resource, token)
				r.record(resource, "error", start)
				return nil, err
			}
		}

		select {
		case <-waitCtx.Done():
			_ = r.removeWaiter(context.Background(), resource, token)
			outcome := "timeout"
			if ctx.Err() != nil {
				outcome = "canceled"
			}
			r.record(resource, outcome, start)
			return nil, waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func (r *RedisLocker) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

func (r *RedisLocker) Ping(ctx context.Context) error {
	if r == nil || r.client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.cfg.CommandTimeout)
	defer cancel()
	return r.client.Ping(ctx).Err()
}

func (r *RedisLocker) removeWaiter(ctx context.Context, resource string, token string) error {
	if err := r.client.LRem(ctx, r.queueKey(resource), 0, token).Err(); err != nil {
		return err
	}
	return r.client.Del(ctx, r.waitKey(resource, token)).Err()
}

func (r *RedisLocker) dropStaleHead(ctx context.Context, resource string, head string) error {
	lockOwner, err := r.client.Get(ctx, r.lockKey(resource)).Result()
	if err != nil && err != redis.Nil {
		return err
	}
	if lockOwner != "" {
		return nil
	}

	exists, err := r.client.Exists(ctx, r.waitKey(resource, head)).Result()
	if err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}

	_, err = r.client.LPop(ctx, r.queueKey(resource)).Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	return err
}

func (r *RedisLocker) record(resource string, outcome string, start time.Time) {
	observe(r.recorder, telemetry.CoordinationMetric{
		AppName:  r.cfg.AppName,
		Provider: r.cfg.Provider,
		Resource: resource,
		Outcome:  outcome,
		WaitMs:   time.Since(start).Milliseconds(),
	})
}

func (r *RedisLocker) lockKey(resource string) string {
	return r.key("lock:" + resource)
}

func (r *RedisLocker) queueKey(resource string) string {
	return r.key("queue:" + resource)
}

func (r *RedisLocker) waitKey(resource string, token string) string {
	return r.key("wait:" + resource + ":" + token)
}

func (r *RedisLocker) key(value string) string {
	prefix := strings.TrimSpace(r.cfg.KeyPrefix)
	if prefix == "" {
		return "tx:" + value
	}
	return prefix + ":tx:" + value
}

func (l *redisLock) Resource() string {
	return l.resource
}

func (l *redisLock) Token() string {
	return l.token
}

func (l *redisLock) Release(ctx context.Context) error {
	var releaseErr error
	l.once.Do(func() {
		close(l.done)
		owner, err := l.parent.client.Get(ctx, l.parent.lockKey(l.resource)).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				return
			}
			releaseErr = err
			return
		}
		if owner != l.token {
			return
		}
		releaseErr = l.parent.client.Del(ctx, l.parent.lockKey(l.resource)).Err()
	})
	return releaseErr
}

func (l *redisLock) startRenewal() {
	interval := l.parent.cfg.RenewInterval
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-l.done:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), l.parent.cfg.CommandTimeout)
				owner, err := l.parent.client.Get(ctx, l.parent.lockKey(l.resource)).Result()
				if err == nil && owner == l.token {
					_ = l.parent.client.Expire(ctx, l.parent.lockKey(l.resource), l.parent.cfg.LeaseTTL).Err()
				}
				cancel()
			}
		}
	}()
}

func (l *redisLock) monitor() {
	timeout := l.parent.cfg.StuckTimeout
	if timeout <= 0 {
		return
	}
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			observe(l.parent.recorder, telemetry.CoordinationMetric{
				AppName:  l.parent.cfg.AppName,
				Provider: l.parent.cfg.Provider,
				Resource: l.resource,
				Outcome:  "stuck",
				WaitMs:   timeout.Milliseconds(),
			})
		case <-l.done:
		}
	}()
}
