package cache

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStoreSetGetDeleteByPrefix(t *testing.T) {
	store := NewMemoryStore(time.Minute, time.Minute)
	type model struct {
		Name string `json:"name"`
	}

	if err := store.Set(context.Background(), "k:1", model{Name: "one"}, time.Minute); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if err := store.Set(context.Background(), "k:2", model{Name: "two"}, time.Minute); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	var out model
	found, err := store.Get(context.Background(), "k:1", &out)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !found || out.Name != "one" {
		t.Fatalf("unexpected get result found=%v out=%+v", found, out)
	}

	if err := store.DeleteByPrefix(context.Background(), "k:"); err != nil {
		t.Fatalf("delete by prefix failed: %v", err)
	}

	found, err = store.Get(context.Background(), "k:1", &out)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if found {
		t.Fatalf("expected key deleted by prefix")
	}
}

func TestMemoryStoreListKeys(t *testing.T) {
	store := NewMemoryStore(time.Minute, time.Minute)

	if err := store.Set(context.Background(), "rbac:role:2", map[string]any{"ok": true}, time.Minute); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if err := store.Set(context.Background(), "rbac:role:1", map[string]any{"ok": true}, time.Minute); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	if err := store.Set(context.Background(), "other:1", map[string]any{"ok": true}, time.Minute); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	keys, total, err := store.ListKeys(context.Background(), "rbac:", 1, 0)
	if err != nil {
		t.Fatalf("list keys failed: %v", err)
	}

	if total != 2 {
		t.Fatalf("expected total=2, got %d", total)
	}
	if len(keys) != 1 || keys[0] != "rbac:role:1" {
		t.Fatalf("unexpected keys page: %+v", keys)
	}
}

func TestMemoryStoreAllowSlidingWindow(t *testing.T) {
	store := NewMemoryStore(time.Minute, time.Minute)
	ctx := context.Background()
	now := time.Unix(100, 0)

	first, err := store.AllowSlidingWindow(ctx, "rate:test", 2, time.Second, now)
	if err != nil {
		t.Fatalf("first rate check failed: %v", err)
	}
	if !first.Allowed || first.Count != 1 || first.Remaining != 1 {
		t.Fatalf("unexpected first result: %+v", first)
	}

	second, err := store.AllowSlidingWindow(ctx, "rate:test", 2, time.Second, now.Add(100*time.Millisecond))
	if err != nil {
		t.Fatalf("second rate check failed: %v", err)
	}
	if !second.Allowed || second.Count != 2 || second.Remaining != 0 {
		t.Fatalf("unexpected second result: %+v", second)
	}

	third, err := store.AllowSlidingWindow(ctx, "rate:test", 2, time.Second, now.Add(200*time.Millisecond))
	if err != nil {
		t.Fatalf("third rate check failed: %v", err)
	}
	if third.Allowed || third.Count != 2 || third.RetryAfter <= 0 {
		t.Fatalf("expected third request to be limited, got %+v", third)
	}

	afterWindow, err := store.AllowSlidingWindow(ctx, "rate:test", 2, time.Second, now.Add(1100*time.Millisecond))
	if err != nil {
		t.Fatalf("post-window rate check failed: %v", err)
	}
	if !afterWindow.Allowed || afterWindow.Count != 1 {
		t.Fatalf("expected post-window request to be allowed after old hits expire, got %+v", afterWindow)
	}
}
