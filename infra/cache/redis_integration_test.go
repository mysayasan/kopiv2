package cache

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestRedisStoreIntegration(t *testing.T) {
	if os.Getenv("RUN_REDIS_IT") != "1" {
		t.Skip("set RUN_REDIS_IT=1 to run Redis integration test")
	}

	store := NewRedisStore(RedisConfig{
		Address:          getenvDefault("REDIS_ADDR", "localhost:6379"),
		Password:         getenvDefault("REDIS_PASSWORD", "Simpnify@123"),
		DB:               0,
		UseTLS:           false,
		KeyPrefix:        "kopiv2-it",
		ConnectTimeout:   2 * time.Second,
		OperationTimeout: 2 * time.Second,
	})
	defer store.Close()

	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("redis ping failed: %v", err)
	}

	type payload struct {
		Role int64 `json:"role"`
	}

	key := "rbac:role:1"
	if err := store.Set(context.Background(), key, payload{Role: 1}, time.Minute); err != nil {
		t.Fatalf("redis set failed: %v", err)
	}

	var out payload
	found, err := store.Get(context.Background(), key, &out)
	if err != nil {
		t.Fatalf("redis get failed: %v", err)
	}
	if !found || out.Role != 1 {
		t.Fatalf("unexpected redis get result found=%v out=%+v", found, out)
	}

	if err := store.DeleteByPrefix(context.Background(), "rbac:"); err != nil {
		t.Fatalf("redis delete by prefix failed: %v", err)
	}

	found, err = store.Get(context.Background(), key, &out)
	if err != nil {
		t.Fatalf("redis get failed: %v", err)
	}
	if found {
		t.Fatalf("expected key deleted by prefix")
	}

	if err := store.Set(context.Background(), "rbac:role:2", payload{Role: 2}, time.Minute); err != nil {
		t.Fatalf("redis set failed: %v", err)
	}
	if err := store.Set(context.Background(), "rbac:role:1", payload{Role: 1}, time.Minute); err != nil {
		t.Fatalf("redis set failed: %v", err)
	}

	keys, total, err := store.ListKeys(context.Background(), "rbac:", 1, 0)
	if err != nil {
		t.Fatalf("redis list keys failed: %v", err)
	}
	if total < 2 {
		t.Fatalf("expected at least 2 keys, got %d", total)
	}
	if len(keys) != 1 || keys[0] != "rbac:role:1" {
		t.Fatalf("unexpected redis keys page: %+v", keys)
	}
}

func getenvDefault(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
