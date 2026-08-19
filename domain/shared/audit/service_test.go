package audit

import (
	"context"
	"errors"
	"testing"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeSortingRepo is an in-memory stand-in that records inserts and applies the
// Action/TargetType/TargetId equality filters + newest-first ordering the service asks for.
//
// Distinct from retention_test.go's fakeAuditRepo, which models the other half of the
// contract: that one honours CreatedAt range filters and Delete but ignores sort order,
// this one honours the sorter but has no Delete. Keeping them separate keeps each test
// asserting against a fake that cannot accidentally satisfy it for the wrong reason.
type fakeSortingRepo struct {
	dbsql.IGenericRepo[AuditLog]
	rows      []*AuditLog
	nextID    int64
	createErr error
}

func (f *fakeSortingRepo) Create(_ context.Context, _ string, m AuditLog) (uint64, error) {
	if f.createErr != nil {
		return 0, f.createErr
	}
	f.nextID++
	m.Id = f.nextID
	cp := m
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}

func (f *fakeSortingRepo) Get(_ context.Context, _ string, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*AuditLog, uint64, error) {
	match := func(r *AuditLog) bool {
		for _, fl := range filters {
			switch fl.FieldName {
			case "Action":
				if r.Action != fl.Value.(string) {
					return false
				}
			case "TargetType":
				if r.TargetType != fl.Value.(string) {
					return false
				}
			case "TargetId":
				if r.TargetId != fl.Value.(string) {
					return false
				}
			}
		}
		return true
	}
	var out []*AuditLog
	for _, r := range f.rows {
		if match(r) {
			out = append(out, r)
		}
	}
	// Newest-first (Id desc) to mirror the DB sorter the service requests.
	if len(sorters) == 1 && sorters[0].FieldName == "Id" && sorters[0].Sort == sqldataenums.DESC {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, uint64(len(out)), nil
}

func newTestAudit() (*service, *fakeSortingRepo) {
	repo := &fakeSortingRepo{}
	return &service{repo: repo}, repo
}

func TestAuditRecordPersistsWithDefaultsAndMetadata(t *testing.T) {
	svc, repo := newTestAudit()
	svc.Record(context.Background(), Entry{
		Action:     "node.adopt",
		ActorId:    5,
		ActorEmail: "op@example.com",
		TargetType: "node",
		TargetId:   "node-7",
		Detail:     "adopted node",
		Metadata:   map[string]any{"ip": "10.0.0.9"},
	})
	if len(repo.rows) != 1 {
		t.Fatalf("expected 1 audit row, got %d", len(repo.rows))
	}
	got := repo.rows[0]
	if got.Outcome != "success" {
		t.Fatalf("Outcome should default to success, got %q", got.Outcome)
	}
	if got.CreatedAt == 0 {
		t.Fatal("CreatedAt should be stamped")
	}
	if got.Metadata != `{"ip":"10.0.0.9"}` {
		t.Fatalf("Metadata not marshaled as expected: %q", got.Metadata)
	}
}

func TestAuditRecordNeverPanicsOrPropagatesOnWriteFailure(t *testing.T) {
	repo := &fakeSortingRepo{createErr: errors.New("db down")}
	logged := 0
	svc := &service{repo: repo, logf: func(string, ...any) { logged++ }}
	// A write failure must be swallowed (best-effort) so auditing never blocks the
	// audited action — the call simply returns.
	svc.Record(context.Background(), Entry{Action: "rbac.elevate", TargetType: "user", TargetId: "3"})
	if logged != 1 {
		t.Fatalf("expected the write failure to be logged once, got %d", logged)
	}
}

func TestAuditListFiltersAndOrdersNewestFirst(t *testing.T) {
	svc, _ := newTestAudit()
	ctx := context.Background()
	svc.Record(ctx, Entry{Action: "node.adopt", TargetType: "node", TargetId: "n1"})
	svc.Record(ctx, Entry{Action: "node.command", TargetType: "node", TargetId: "n1"})
	svc.Record(ctx, Entry{Action: "rbac.set_role", TargetType: "user", TargetId: "9"})

	// No filter: all three, newest-first.
	all, total, err := svc.List(ctx, 0, 0, Filter{})
	if err != nil || total != 3 || len(all) != 3 {
		t.Fatalf("List all: total=%d len=%d err=%v", total, len(all), err)
	}
	if all[0].Action != "rbac.set_role" {
		t.Fatalf("expected newest (rbac.set_role) first, got %q", all[0].Action)
	}

	// Filter by target type + id.
	nodeRows, _, _ := svc.List(ctx, 0, 0, Filter{TargetType: "node", TargetId: "n1"})
	if len(nodeRows) != 2 {
		t.Fatalf("expected 2 node/n1 rows, got %d", len(nodeRows))
	}

	// Filter by action.
	roleRows, _, _ := svc.List(ctx, 0, 0, Filter{Action: "rbac.set_role"})
	if len(roleRows) != 1 || roleRows[0].TargetId != "9" {
		t.Fatalf("action filter wrong: %+v", roleRows)
	}
}
