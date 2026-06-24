package services

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeAlertRepo is a minimal in-memory IGenericRepo for the methods PurgeAlerts
// touches (Get with CreatedAt/IsDiagnostic filters + ASC sort + paging, and
// DeleteById). Other methods are unimplemented and panic if called.
type fakeAlertRepo struct {
	dbsql.IGenericRepo[entities.AlertEvent]
	rows []*entities.AlertEvent
}

func (f *fakeAlertRepo) Get(_ context.Context, _ string, limit uint64, offset uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.AlertEvent, uint64, error) {
	var out []*entities.AlertEvent
	for _, r := range f.rows {
		keep := true
		for _, fl := range filters {
			switch fl.FieldName {
			case "CreatedAt":
				cutoff, _ := fl.Value.(int64)
				if !(r.CreatedAt < cutoff) {
					keep = false
				}
			case "IsDiagnostic":
				want, _ := fl.Value.(bool)
				if r.IsDiagnostic != want {
					keep = false
				}
			}
		}
		if keep {
			cp := *r
			out = append(out, &cp)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	if offset >= uint64(len(out)) {
		return nil, uint64(len(out)), nil
	}
	out = out[offset:]
	if limit > 0 && uint64(len(out)) > limit {
		out = out[:limit]
	}
	return out, uint64(len(out)), nil
}

func (f *fakeAlertRepo) DeleteById(_ context.Context, _ string, id uint64) (uint64, error) {
	for i, r := range f.rows {
		if r.Id == int64(id) {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeAlertRepo) ids() []int64 {
	out := make([]int64, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, r.Id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func TestPurgeAlertsOnlyDiagnostics(t *testing.T) {
	now := time.Now().UTC().Unix()
	repo := &fakeAlertRepo{rows: []*entities.AlertEvent{
		{Id: 1, CreatedAt: now - 100, IsDiagnostic: true},
		{Id: 2, CreatedAt: now - 100, IsDiagnostic: false}, // real detection — keep
		{Id: 3, CreatedAt: now - 10, IsDiagnostic: true},
		{Id: 4, CreatedAt: now - 5, IsDiagnostic: false}, // real detection — keep
	}}
	svc := &visionService{alerts: repo}

	deleted, err := svc.PurgeAlertsOlderThanDays(context.Background(), 0, true)
	if err != nil {
		t.Fatalf("purge failed: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2 (both diagnostics)", deleted)
	}
	got := repo.ids()
	if len(got) != 2 || got[0] != 2 || got[1] != 4 {
		t.Fatalf("remaining ids = %v, want [2 4] (real detections kept)", got)
	}
}

func TestPurgeAlertsRetentionDeletesAllOlderRows(t *testing.T) {
	now := time.Now().UTC().Unix()
	old := now - 10*24*3600 // 10 days ago
	repo := &fakeAlertRepo{rows: []*entities.AlertEvent{
		{Id: 1, CreatedAt: old, IsDiagnostic: false},
		{Id: 2, CreatedAt: old, IsDiagnostic: true},
		{Id: 3, CreatedAt: now - 3600, IsDiagnostic: false}, // recent — keep
	}}
	svc := &visionService{alerts: repo}

	deleted, err := svc.PurgeAlertsOlderThanDays(context.Background(), 7, false)
	if err != nil {
		t.Fatalf("purge failed: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2 (both older than 7 days)", deleted)
	}
	if got := repo.ids(); len(got) != 1 || got[0] != 3 {
		t.Fatalf("remaining ids = %v, want [3]", got)
	}
}
