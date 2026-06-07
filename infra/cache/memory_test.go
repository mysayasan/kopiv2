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
