package services

import (
	"context"
	"sync"
	"time"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// RelayDedup deduplicates node-originated notifications the control plane ingests, so replaying
// a node's missed notifications on reconnect never double-inserts one already delivered live (or
// by an earlier replay). It keys on the node's stable engine id, which travels on BOTH the live
// control-channel push and the persisted row a replay pull returns.
type RelayDedup struct {
	repo dbsql.IGenericRepo[entities.RelayedNotif]

	// Per-node lock serialises the check-then-record for one node, so a live push and a
	// concurrent replay of the same event can't both decide it's new and publish it twice.
	// Contention is low: a node's events arrive on its one control conn plus its one replay
	// goroutine.
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewRelayDedup builds the dedup ledger over the control plane's own database.
func NewRelayDedup(db dbsql.IDbCrud) *RelayDedup {
	return &RelayDedup{repo: dbsql.NewGenericRepo[entities.RelayedNotif](db), locks: map[string]*sync.Mutex{}}
}

func (d *RelayDedup) nodeLock(nodeID string) *sync.Mutex {
	d.mu.Lock()
	defer d.mu.Unlock()
	l := d.locks[nodeID]
	if l == nil {
		l = &sync.Mutex{}
		d.locks[nodeID] = l
	}
	return l
}

// SeenOrRecord reports whether (nodeID, originID) has already been ingested. On a first sighting
// it records the marker and returns false (caller should ingest). An empty originID cannot be
// deduped, so it returns false (best-effort: ingest it — the worst case is a rare duplicate when
// replaying a node too old to carry engine ids).
func (d *RelayDedup) SeenOrRecord(ctx context.Context, nodeID, originID string, createdAt int64) bool {
	if originID == "" {
		return false
	}
	l := d.nodeLock(nodeID)
	l.Lock()
	defer l.Unlock()

	key := nodeID + "|" + originID
	if existing, err := d.repo.GetByUnique(ctx, "", "dedup_key", key); err == nil && existing != nil {
		return true
	}
	if createdAt == 0 {
		createdAt = time.Now().Unix()
	}
	_, _ = d.repo.Create(ctx, "", entities.RelayedNotif{DedupKey: key, NodeId: nodeID, CreatedAt: createdAt})
	return false
}

// Prune drops dedup markers older than olderThan (unix seconds). The replay pull only reaches
// back a bounded window, so a marker older than that can never be re-offered and is safe to drop.
func (d *RelayDedup) Prune(ctx context.Context, olderThan int64) (int, error) {
	n, err := d.repo.Delete(ctx, "", []sqldataenums.Filter{
		{FieldName: "CreatedAt", Compare: sqldataenums.LessThan, Value: olderThan},
	})
	return int(n), err
}
