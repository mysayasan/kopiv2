package services

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/infra/cache"
)

// Which instance is holding a given node's control channel?
//
// Behind a load balancer that question has no local answer. A node dials IN and keeps one
// connection open to whichever instance it reached, so every instance knows only its own
// nodes. Two things break on that alone, and neither is loud:
//
//   - Liveness. The heartbeat reconciler treats "not in my connection map" as "not
//     control-connected" and falls back to an mTLS probe. Where that probe cannot reach the
//     node (NAT, firewall) a perfectly healthy node attached to another instance is marked
//     lost and an operator is alerted about it.
//   - Commands. Opening a node's own screens only works if the request happened to land on
//     the instance holding that node — a 1-in-N coin flip.
//
// The registry makes ownership a deployment-wide fact: the instance holding a connection
// publishes a claim, and every instance can read it. It is stored in the shared cache
// rather than the database on purpose — a claim is a LEASE, and a TTL that expires on its
// own is exactly the right behaviour when an instance dies without cleaning up. A database
// row would need a reaper, and a reaper that fails leaves nodes permanently owned by a
// machine that no longer exists.
//
// With the per-process (memory) cache this is a map inside one process, which is precisely
// correct for a single instance: it owns every connection it knows about, and there is
// nobody to forward to.

// ownerKeyPrefix namespaces control-channel claims inside the shared cache.
const ownerKeyPrefix = "nodeowner:"

// mediaOwnerKeyPrefix namespaces MEDIA-channel claims.
//
// A node holds TWO independent connections to the control plane — the control channel for
// commands and a separate media channel for camera RTP — and nothing guarantees they land
// on the same instance. Tracking them under one key would let a command be forwarded to an
// instance that holds only the media connection, or a video request to one that holds only
// the control channel, and both would fail in a way that looks like the node being down.
const mediaOwnerKeyPrefix = "mediaowner:"

// defaultOwnershipTTL bounds how long a claim survives unrenewed — and so how long a dead
// instance keeps appearing to own its nodes.
const defaultOwnershipTTL = 30 * time.Second

// NodeOwnerRegistry publishes and resolves which instance holds each node's control channel.
type NodeOwnerRegistry struct {
	store cache.Store
	// prefix namespaces this registry's claims, so the control channel and the media
	// channel are tracked separately (a node holds both, and not necessarily on the same
	// instance).
	prefix string
	// self is this instance's advertise URL, the value written into a claim. Empty means
	// forwarding is not configured, which makes the registry a no-op.
	self string
	ttl  time.Duration
	logf func(string, ...any)

	// held is the set of nodes THIS instance currently has connections for. The renewal
	// loop walks it; it is also the fast path for "is this mine?" without a cache round
	// trip on every request.
	mu   sync.RWMutex
	held map[string]struct{}
}

// NewNodeOwnerRegistry builds a registry. A nil store or an empty self disables it — every
// method then behaves as "only local connections exist", the single-instance truth.
func NewNodeOwnerRegistry(store cache.Store, self string, ttl time.Duration, logf func(string, ...any)) *NodeOwnerRegistry {
	return newOwnerRegistry(store, ownerKeyPrefix, self, ttl, logf)
}

// NewMediaOwnerRegistry tracks which instance holds each node's MEDIA channel. Same
// mechanism, separate namespace — see mediaOwnerKeyPrefix.
func NewMediaOwnerRegistry(store cache.Store, self string, ttl time.Duration, logf func(string, ...any)) *NodeOwnerRegistry {
	return newOwnerRegistry(store, mediaOwnerKeyPrefix, self, ttl, logf)
}

func newOwnerRegistry(store cache.Store, prefix, self string, ttl time.Duration, logf func(string, ...any)) *NodeOwnerRegistry {
	if ttl <= 0 {
		ttl = defaultOwnershipTTL
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &NodeOwnerRegistry{
		store:  store,
		prefix: prefix,
		self:   strings.TrimSpace(self),
		ttl:    ttl,
		logf:   logf,
		held:   map[string]struct{}{},
	}
}

// Enabled reports whether ownership is actually published anywhere.
func (r *NodeOwnerRegistry) Enabled() bool {
	return r != nil && r.store != nil && r.self != ""
}

// Claim records that this instance now holds nodeID's control channel, and starts renewing
// it. Called when a control connection is accepted.
//
// Deliberately last-writer-wins rather than a compare-and-set: a node holds exactly one
// connection, so if it has just reconnected to a different instance, that instance IS the
// new owner and should overwrite. Refusing the write would leave the claim pointing at an
// instance that no longer has the connection — the failure this exists to prevent.
func (r *NodeOwnerRegistry) Claim(ctx context.Context, nodeID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.held[nodeID] = struct{}{}
	r.mu.Unlock()

	if !r.Enabled() {
		return
	}
	if err := r.store.Set(ctx, r.key(nodeID), r.self, r.ttl); err != nil {
		// Not fatal: the node is connected here regardless, so local requests still work.
		// Only the other instances' view is degraded, and the claim retries on the next
		// renewal tick.
		r.logf("node ownership claim for %s failed: %v", nodeID, err)
	}
}

// Release withdraws this instance's claim when a control connection drops, so another
// instance can take over without waiting out the TTL.
//
// It deletes only if the claim still points HERE. A node that reconnected elsewhere already
// overwrote the key, and deleting then would erase the new owner's claim and make a
// perfectly reachable node look unowned.
func (r *NodeOwnerRegistry) Release(ctx context.Context, nodeID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.held, nodeID)
	r.mu.Unlock()

	if !r.Enabled() {
		return
	}
	owner, ok := r.lookup(ctx, nodeID)
	if !ok || owner != r.self {
		return
	}
	if err := r.store.Delete(ctx, r.key(nodeID)); err != nil {
		r.logf("node ownership release for %s failed: %v", nodeID, err)
	}
}

// OwnerOf returns the advertise URL of the instance holding nodeID's control channel, and
// whether that instance is THIS one. An empty URL with local=false means nobody holds it.
func (r *NodeOwnerRegistry) OwnerOf(ctx context.Context, nodeID string) (owner string, local bool) {
	if r == nil {
		return "", false
	}
	r.mu.RLock()
	_, mine := r.held[nodeID]
	r.mu.RUnlock()
	if mine {
		return r.self, true
	}
	if !r.Enabled() {
		return "", false
	}
	found, ok := r.lookup(ctx, nodeID)
	if !ok {
		return "", false
	}
	return found, found == r.self
}

// ConnectedAnywhere reports whether ANY instance holds this node's control channel.
//
// This is the liveness oracle the heartbeat reconciler needs. Without it the reconciler
// reads its own partial view as the whole truth and marks healthy nodes lost.
func (r *NodeOwnerRegistry) ConnectedAnywhere(ctx context.Context, nodeID string) bool {
	owner, local := r.OwnerOf(ctx, nodeID)
	// `local` is checked separately rather than inferred from a non-empty owner: a
	// standalone instance has no advertise URL, so it holds connections under an EMPTY
	// self. Reading only the URL would report every node on every single-instance install
	// as not-connected, which is the whole failure this oracle exists to prevent — the
	// heartbeat would fall back to the mTLS probe for the entire fleet.
	return local || owner != ""
}

// lookup reads a claim from the shared cache.
func (r *NodeOwnerRegistry) lookup(ctx context.Context, nodeID string) (string, bool) {
	var owner string
	found, err := r.store.Get(ctx, r.key(nodeID), &owner)
	if err != nil || !found {
		return "", false
	}
	owner = strings.TrimSpace(owner)
	return owner, owner != ""
}

// StartRenewal re-publishes every claim this instance holds, until ctx is cancelled.
//
// Renewal is what makes the TTL safe to keep short: a live instance keeps its claims fresh,
// and a dead one stops, so its nodes become claimable within one TTL. The interval is a
// third of the TTL so a single missed tick — a slow cache round trip, a GC pause — cannot
// expire a claim that is genuinely still held.
func (r *NodeOwnerRegistry) StartRenewal(ctx context.Context) {
	if !r.Enabled() {
		return
	}
	interval := r.ttl / 3
	if interval <= 0 {
		interval = time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.renewAll(ctx)
			}
		}
	}()
}

func (r *NodeOwnerRegistry) renewAll(ctx context.Context) {
	r.mu.RLock()
	nodes := make([]string, 0, len(r.held))
	for id := range r.held {
		nodes = append(nodes, id)
	}
	r.mu.RUnlock()

	for _, id := range nodes {
		if err := r.store.Set(ctx, r.key(id), r.self, r.ttl); err != nil {
			r.logf("node ownership renewal for %s failed: %v", id, err)
		}
	}
}

// key namespaces one node's claim for this registry.
func (r *NodeOwnerRegistry) key(nodeID string) string { return r.prefix + nodeID }
