package services

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The coordination lock is editable beside the cache because the two answer different
// halves of one question. The cache decides whether SESSIONS are shared — get that wrong
// and users are signed out as the balancer moves them, which is at least visible. The lock
// decides whether SCHEDULED WORK runs once for the deployment or once per instance, and
// getting that wrong is silent: for myidsan it means several instances trimming the audit
// trail at the same moment, each writing its own partial archive to its own disk.
//
// Exposing only the cache would let an operator reach that state by answering the half of
// the question they were offered.
func TestStorageSectionExposesLockProvider(t *testing.T) {
	svc, _, _ := newIdsanSettings(t)

	values, err := svc.Get("storage")
	if err != nil {
		t.Fatalf("Get(storage): %v", err)
	}
	tx, ok := values["transaction"].(map[string]any)
	if !ok {
		t.Fatal("the storage section must expose the transaction block beside the cache")
	}
	if _, ok := tx["lockProvider"]; !ok {
		t.Fatal("transaction.lockProvider must be readable, or the wizard cannot move it with the cache")
	}
}

// Saving must reach BOTH the live config and the file on disk: the running process needs
// the new value, and the file is the only thing the next boot reads.
func TestSavingLockProviderAppliesAndMaterializes(t *testing.T) {
	svc, cfg, path := newIdsanSettings(t)

	current, err := svc.Get("storage")
	if err != nil {
		t.Fatalf("Get(storage): %v", err)
	}
	// The fixture config carries no fileStorage block, and the section refuses an empty
	// path — supply one so the assertion below is about the lock provider and nothing else.
	current["fileStorage"] = map[string]any{"path": "./uploads"}
	tx, _ := current["transaction"].(map[string]any)
	tx["lockProvider"] = "redis"
	current["transaction"] = tx

	body, err := json.Marshal(current)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := svc.Save(context.Background(), "storage", body); err != nil {
		t.Fatalf("Save(storage): %v", err)
	}

	if cfg.Transaction.LockProvider != "redis" {
		t.Fatalf("live config lock provider = %q, want redis", cfg.Transaction.LockProvider)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(saved, &doc); err != nil {
		t.Fatalf("parse saved config: %v", err)
	}
	block, ok := doc["transaction"].(map[string]any)
	if !ok {
		t.Fatal("the saved config must carry a transaction block, or the value is lost on restart")
	}
	if block["lockProvider"] != "redis" {
		t.Fatalf("saved lock provider = %v, want redis", block["lockProvider"])
	}
	// Untouched blocks must survive the surgical rewrite.
	if _, ok := doc["audit"]; !ok {
		t.Fatal("writing the transaction block must not drop other config blocks")
	}
}

// A value the host cannot turn into a locker aborts the boot. Catching it at save time
// costs an error message; catching it at boot time costs a process that will not come
// back up, discovered by whoever restarted it.
func TestUnknownLockProviderIsRefused(t *testing.T) {
	svc, _, _ := newIdsanSettings(t)

	current, err := svc.Get("storage")
	if err != nil {
		t.Fatalf("Get(storage): %v", err)
	}
	// The fixture config carries no fileStorage block, and the section refuses an empty
	// path — supply one so the assertion below is about the lock provider and nothing else.
	current["fileStorage"] = map[string]any{"path": "./uploads"}
	tx, _ := current["transaction"].(map[string]any)
	tx["lockProvider"] = "etcd"
	current["transaction"] = tx

	body, _ := json.Marshal(current)
	_, err = svc.Save(context.Background(), "storage", body)
	if err == nil {
		t.Fatal("an unsupported lock provider must be refused, not saved into a config that cannot boot")
	}
	// Asserted on the message because this section has other validation that would also
	// reject the payload — a bare "an error happened" would pass whether or not the lock
	// provider was ever checked.
	if !strings.Contains(err.Error(), "lock provider") {
		t.Fatalf("got %q, want the failure to name the lock provider", err)
	}
}
