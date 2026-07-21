package services

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/mysayasan/kopiv2/apps/myseliasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// in-memory fake of the RelayedNotif repo (only the methods RelayDedup uses).
type fakeRelayRepo struct {
	dbsql.IGenericRepo[entities.RelayedNotif]
	rows   []*entities.RelayedNotif
	nextID int64
}

func (f *fakeRelayRepo) GetByUnique(_ context.Context, _ string, _ string, uids ...any) (*entities.RelayedNotif, error) {
	key, _ := uids[0].(string)
	for _, r := range f.rows {
		if r.DedupKey == key {
			cp := *r
			return &cp, nil
		}
	}
	return nil, errors.New("no result found")
}
func (f *fakeRelayRepo) Create(_ context.Context, _ string, m entities.RelayedNotif) (uint64, error) {
	f.nextID++
	m.Id = f.nextID
	cp := m
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}
func (f *fakeRelayRepo) Delete(_ context.Context, _ string, filters []sqldataenums.Filter) (uint64, error) {
	cutoff, _ := filters[0].Value.(int64)
	kept := f.rows[:0:0]
	var deleted uint64
	for _, r := range f.rows {
		if r.CreatedAt < cutoff {
			deleted++
			continue
		}
		kept = append(kept, r)
	}
	f.rows = kept
	return deleted, nil
}

func newTestDedup() (*RelayDedup, *fakeRelayRepo) {
	repo := &fakeRelayRepo{}
	return &RelayDedup{repo: repo, locks: map[string]*sync.Mutex{}}, repo
}

func TestRelayDedupSeenOrRecord(t *testing.T) {
	d, repo := newTestDedup()
	ctx := context.Background()

	// First sighting of an event => not seen (ingest), and it's recorded.
	if d.SeenOrRecord(ctx, "nodeA", "oid-1", 100) {
		t.Fatal("first sighting should be new")
	}
	if len(repo.rows) != 1 {
		t.Fatalf("expected 1 marker, got %d", len(repo.rows))
	}
	// Same (node, origin) again => seen (skip), no new marker.
	if !d.SeenOrRecord(ctx, "nodeA", "oid-1", 100) {
		t.Fatal("second sighting should be seen")
	}
	if len(repo.rows) != 1 {
		t.Fatalf("duplicate must not add a marker, got %d", len(repo.rows))
	}
	// Different origin id => new.
	if d.SeenOrRecord(ctx, "nodeA", "oid-2", 100) {
		t.Fatal("different origin id should be new")
	}
	// Same origin id but a DIFFERENT node => new (keyed by node+origin).
	if d.SeenOrRecord(ctx, "nodeB", "oid-1", 100) {
		t.Fatal("same origin id on a different node should be new")
	}
	// Empty origin id can't be deduped => always new (best-effort ingest), never recorded.
	before := len(repo.rows)
	if d.SeenOrRecord(ctx, "nodeA", "", 100) {
		t.Fatal("empty origin id should be treated as new")
	}
	if len(repo.rows) != before {
		t.Fatal("empty origin id must not be recorded")
	}
}

func TestRelayDedupPrune(t *testing.T) {
	d, repo := newTestDedup()
	ctx := context.Background()
	d.SeenOrRecord(ctx, "n", "old", 100)
	d.SeenOrRecord(ctx, "n", "new", 500)

	n, err := d.Prune(ctx, 300) // drop markers created before ts=300
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 || len(repo.rows) != 1 || repo.rows[0].DedupKey != "n|new" {
		t.Fatalf("prune dropped the wrong rows: deleted=%d rows=%+v", n, repo.rows)
	}
}
