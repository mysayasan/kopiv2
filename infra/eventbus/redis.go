package eventbus

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisBus fans out across instances with Redis publish/subscribe.
//
// Publish/subscribe rather than a stream or a queue on purpose. Both consumers want the
// SAME event delivered to EVERY instance — the notification bell renders it to whichever
// browsers that instance is serving, and the correlator folds it into its own matching
// state. A queue would hand each event to exactly one consumer, which is the opposite of
// what is needed; a stream would add durable retention for data whose durable copy is
// already the database.
type RedisBus struct {
	client *redis.Client
	cfg    Config
}

func NewRedisBus(cfg Config) *RedisBus {
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 2 * time.Second
	}
	if cfg.CommandTimeout <= 0 {
		cfg.CommandTimeout = 2 * time.Second
	}
	var tlsConfig *tls.Config
	if cfg.RedisUseTLS {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return &RedisBus{
		client: redis.NewClient(&redis.Options{
			Addr:        cfg.RedisAddress,
			Password:    cfg.RedisPassword,
			DB:          cfg.RedisDB,
			DialTimeout: cfg.ConnectTimeout,
			// Read/write timeouts are deliberately NOT set from CommandTimeout: a
			// subscription is a long-lived read that must stay open between events, and a
			// read deadline would tear it down on every quiet period.
			TLSConfig: tlsConfig,
		}),
		cfg: cfg,
	}
}

func (r *RedisBus) Distributed() bool { return true }

func (r *RedisBus) Publish(ctx context.Context, topic string, payload []byte) error {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.CommandTimeout)
	defer cancel()
	return r.client.Publish(ctx, topicKey(r.cfg.KeyPrefix, r.cfg.AppName, topic), payload).Err()
}

// Subscribe delivers every message published to topic until ctx is cancelled.
//
// go-redis reconnects a dropped subscription by itself, so a Redis restart costs the
// events published while it was down — at-most-once, as documented — rather than the
// subscription itself. Losing the subscription silently would be far worse: the instance
// would keep serving, with a bell that never updates and rules that never match.
func (r *RedisBus) Subscribe(ctx context.Context, topic string, handler Handler) error {
	if handler == nil {
		return nil
	}
	sub := r.client.Subscribe(ctx, topicKey(r.cfg.KeyPrefix, r.cfg.AppName, topic))
	ch := sub.Channel()

	go func() {
		defer sub.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				handler([]byte(msg.Payload))
			}
		}
	}()
	return nil
}

func (r *RedisBus) Ping(ctx context.Context) error {
	if r == nil || r.client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.cfg.CommandTimeout)
	defer cancel()
	return r.client.Ping(ctx).Err()
}

func (r *RedisBus) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}
