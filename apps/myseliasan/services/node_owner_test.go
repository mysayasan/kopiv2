package services

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/infra/cache"
)

// fakeOwnerStore is a minimal cache.Store covering the key-value + TTL methods the owner
// registry uses, with a clock the test drives so lease expiry is deterministic.
type fakeOwnerStore struct {
	mu     sync.Mutex
	values map[string]string
	expiry map[string]time.Time
	now    time.Time
	setErr error
}

func newFakeOwnerStore() *fakeOwnerStore {
	return &fakeOwnerStore{
		values: map[string]string{},
		expiry: map[string]time.Time{},
		now:    time.Unix(1_700_000_000, 0),
	}
}

func (f *fakeOwnerStore) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func (f *fakeOwnerStore) Get(_ context.Context, key string, dest any) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	exp, ok := f.expiry[key]
	if !ok || !f.now.Before(exp) {
		return false, nil
	}
	if p, ok := dest.(*string); ok {
		*p = f.values[key]
	}
	return true, nil
}

func (f *fakeOwnerStore) Set(_ context.Context, key string, value any, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	f.values[key] = value.(string)
	f.expiry[key] = f.now.Add(ttl)
	return nil
}

func (f *fakeOwnerStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.values, key)
	delete(f.expiry, key)
	return nil
}

func (f *fakeOwnerStore) has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	exp, ok := f.expiry[key]
	return ok && f.now.Before(exp)
}

// The rest of cache.Store, unused here.
func (f *fakeOwnerStore) DeleteByPrefix(context.Context, string) error { return nil }
func (f *fakeOwnerStore) ListKeys(context.Context, string, uint64, uint64) ([]string, uint64, error) {
	return nil, 0, nil
}
func (f *fakeOwnerStore) Ping(context.Context) error { return nil }
func (f *fakeOwnerStore) Close() error               { return nil }

func (f *fakeOwnerStore) AllowSlidingWindow(context.Context, string, int64, time.Duration, time.Time) (cache.SlidingWindowResult, error) {
	return cache.SlidingWindowResult{}, nil
}

// A single instance owns everything it is connected to, and there is nobody to forward to.
// This is the shipped default, so it must work with no cluster configuration at all.
func TestOwnerRegistryStandaloneIsLocalOnly(t *testing.T) {
	r := NewNodeOwnerRegistry(nil, "", 0, nil)
	ctx := context.Background()

	if r.Enabled() {
		t.Fatal("an unconfigured registry must not claim to be enabled")
	}
	r.Claim(ctx, "node-a")

	owner, local := r.OwnerOf(ctx, "node-a")
	if !local {
		t.Fatalf("a locally connected node must resolve as local; got owner=%q local=%v", owner, local)
	}
	if !r.ConnectedAnywhere(ctx, "node-a") {
		t.Fatal("a locally connected node must count as connected")
	}
	if r.ConnectedAnywhere(ctx, "node-b") {
		t.Fatal("a node nobody holds must not count as connected")
	}
}

// The distributed case: one instance publishes, another reads. This is what stops the
// heartbeat marking a healthy node lost just because it is attached elsewhere.
func TestOwnerRegistryResolvesAcrossInstances(t *testing.T) {
	store := newFakeOwnerStore()
	ctx := context.Background()

	a := NewNodeOwnerRegistry(store, "https://a:3002", 30*time.Second, nil)
	b := NewNodeOwnerRegistry(store, "https://b:3002", 30*time.Second, nil)

	a.Claim(ctx, "node-1")

	owner, local := b.OwnerOf(ctx, "node-1")
	if owner != "https://a:3002" {
		t.Fatalf("instance B resolved owner %q, want A's advertise URL", owner)
	}
	if local {
		t.Fatal("instance B must not think it owns a node it never connected")
	}
	if !b.ConnectedAnywhere(ctx, "node-1") {
		t.Fatal("B must see the node as connected somewhere — this is the false-lost fix")
	}
}

// A node reconnecting to a different instance must transfer ownership. Refusing the write
// would leave the claim pointing at an instance that no longer holds the connection.
func TestOwnerRegistryReconnectTransfersOwnership(t *testing.T) {
	store := newFakeOwnerStore()
	ctx := context.Background()

	a := NewNodeOwnerRegistry(store, "https://a:3002", 30*time.Second, nil)
	b := NewNodeOwnerRegistry(store, "https://b:3002", 30*time.Second, nil)

	a.Claim(ctx, "node-1")
	b.Claim(ctx, "node-1") // the node reconnected to B

	if owner, _ := b.OwnerOf(ctx, "node-1"); owner != "https://b:3002" {
		t.Fatalf("owner = %q, want B after the reconnect", owner)
	}
}

// Releasing must only delete a claim that still points here. A node that reconnected
// elsewhere already overwrote the key, and deleting then would erase the NEW owner's claim
// and make a perfectly reachable node look unowned.
func TestOwnerRegistryReleaseDoesNotStealFromNewOwner(t *testing.T) {
	store := newFakeOwnerStore()
	ctx := context.Background()

	a := NewNodeOwnerRegistry(store, "https://a:3002", 30*time.Second, nil)
	b := NewNodeOwnerRegistry(store, "https://b:3002", 30*time.Second, nil)

	a.Claim(ctx, "node-1")
	b.Claim(ctx, "node-1")
	// A's old connection now tears down and releases, AFTER B took over.
	a.Release(ctx, "node-1")

	if !store.has(ownerKeyPrefix + "node-1") {
		t.Fatal("the surviving owner's claim was deleted by the previous owner's release")
	}
	if owner, _ := b.OwnerOf(ctx, "node-1"); owner != "https://b:3002" {
		t.Fatalf("owner = %q, want B to still own the node", owner)
	}
}

// Release on the CURRENT owner must free the node immediately, so a takeover does not have
// to wait out the lease.
func TestOwnerRegistryReleaseFreesTheNode(t *testing.T) {
	store := newFakeOwnerStore()
	ctx := context.Background()
	a := NewNodeOwnerRegistry(store, "https://a:3002", 30*time.Second, nil)

	a.Claim(ctx, "node-1")
	a.Release(ctx, "node-1")

	if a.ConnectedAnywhere(ctx, "node-1") {
		t.Fatal("a released node must not still look connected")
	}
}

// An instance that dies cannot release anything, so the lease has to expire on its own —
// otherwise its nodes stay owned by a machine that no longer exists.
func TestOwnerRegistryClaimExpiresWithoutRenewal(t *testing.T) {
	store := newFakeOwnerStore()
	ctx := context.Background()

	dead := NewNodeOwnerRegistry(store, "https://dead:3002", 30*time.Second, nil)
	survivor := NewNodeOwnerRegistry(store, "https://alive:3002", 30*time.Second, nil)
	dead.Claim(ctx, "node-1")

	if !survivor.ConnectedAnywhere(ctx, "node-1") {
		t.Fatal("a fresh claim should be visible")
	}
	store.advance(31 * time.Second) // the owner stopped renewing

	if survivor.ConnectedAnywhere(ctx, "node-1") {
		t.Fatal("a claim from a dead instance must expire so the node can be taken over")
	}
}

// Renewal is what makes a short lease safe: a live instance keeps its claims fresh.
func TestOwnerRegistryRenewalKeepsClaimAlive(t *testing.T) {
	store := newFakeOwnerStore()
	ctx := context.Background()

	a := NewNodeOwnerRegistry(store, "https://a:3002", 30*time.Second, nil)
	a.Claim(ctx, "node-1")

	store.advance(20 * time.Second)
	a.renewAll(ctx) // the loop's tick, driven directly so the test is deterministic
	store.advance(20 * time.Second)

	if !a.ConnectedAnywhere(ctx, "node-1") {
		t.Fatal("a renewed claim must survive past the original TTL")
	}
}

// A cache write can fail while the node is perfectly connected. Local service must not
// depend on the publish succeeding — only the other instances' view is degraded, and the
// renewal loop retries.
func TestOwnerRegistrySurvivesStoreErrors(t *testing.T) {
	store := newFakeOwnerStore()
	store.setErr = errContext
	ctx := context.Background()

	a := NewNodeOwnerRegistry(store, "https://a:3002", 30*time.Second, nil)
	a.Claim(ctx, "node-1")

	if owner, local := a.OwnerOf(ctx, "node-1"); !local || owner != "https://a:3002" {
		t.Fatalf("a locally held node must resolve locally even when publishing failed; got %q local=%v", owner, local)
	}
}

var errContext = context.DeadlineExceeded
