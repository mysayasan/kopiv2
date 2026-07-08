package notification

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case uint64:
		return int64(n)
	default:
		return 0
	}
}

// fakeNotifRepo honors the maintainer's contract: it applies the `Id >` filter,
// sorts ascending by id, and pages by limit — the behaviors Sweep depends on for
// exactly-once, offset-free paging.
type fakeNotifRepo struct {
	dbsql.IGenericRepo[entities.Notification]
	rows []*entities.Notification
}

func (f *fakeNotifRepo) Get(_ context.Context, _ string, limit, offset uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.Notification, uint64, error) {
	minID := int64(-1)
	for _, fl := range filters {
		if fl.FieldName == "Id" && fl.Compare == sqldataenums.GreaterThan {
			minID = toInt64(fl.Value)
		}
	}
	var matched []*entities.Notification
	for _, r := range f.rows {
		if r.Id > minID {
			matched = append(matched, r)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].Id < matched[j].Id })
	total := uint64(len(matched))
	if offset >= total {
		return nil, total, nil
	}
	// Emulate the real generic repo's hard 100-row-per-query cap (db_crud_sel.go):
	// it returns at most this many rows REGARDLESS of the requested limit. A tiny
	// cap here forces multi-page paging on a small fixture and regression-guards the
	// bug where Sweep terminated on a short page instead of an empty one.
	const hardCap = uint64(3)
	if limit > hardCap {
		limit = hardCap
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return matched[offset:end], total, nil
}

// fakeRollupRepo is an in-memory rollup store. Get returns COPIES so a bucket
// count only persists when flush actually calls UpdateById (or Create) — this
// keeps the test faithful to real repositories.
type fakeRollupRepo struct {
	dbsql.IGenericRepo[entities.NotificationRollup]
	rows   []*entities.NotificationRollup
	nextID int64
}

func rollupMatches(r *entities.NotificationRollup, filters []sqldataenums.Filter) bool {
	for _, fl := range filters {
		if fl.Compare != sqldataenums.Equal {
			continue
		}
		switch fl.FieldName {
		case "BucketStart":
			if r.BucketStart != toInt64(fl.Value) {
				return false
			}
		case "CameraId":
			if r.CameraId != toInt64(fl.Value) {
				return false
			}
		case "RuleId":
			if r.RuleId != toInt64(fl.Value) {
				return false
			}
		case "Category":
			if r.Category != fl.Value.(string) {
				return false
			}
		case "Severity":
			if r.Severity != fl.Value.(string) {
				return false
			}
		case "Label":
			if r.Label != fl.Value.(string) {
				return false
			}
		}
	}
	return true
}

func (f *fakeRollupRepo) Get(_ context.Context, _ string, limit, offset uint64, filters []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.NotificationRollup, uint64, error) {
	var matched []*entities.NotificationRollup
	for _, r := range f.rows {
		if rollupMatches(r, filters) {
			cp := *r
			matched = append(matched, &cp)
		}
	}
	total := uint64(len(matched))
	if offset >= total {
		return nil, total, nil
	}
	// Emulate the real repo's hard 100-row-per-query cap (tiny here) so the heatmap
	// read must page by offset to cover all rollup rows; a broken loop that trusts a
	// short page would sum only the first page.
	const hardCap = uint64(2)
	if limit > hardCap {
		limit = hardCap
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return matched[offset:end], total, nil
}

func (f *fakeRollupRepo) Create(_ context.Context, _ string, model entities.NotificationRollup) (uint64, error) {
	f.nextID++
	model.Id = f.nextID
	cp := model
	f.rows = append(f.rows, &cp)
	return uint64(f.nextID), nil
}

func (f *fakeRollupRepo) UpdateById(_ context.Context, _ string, model entities.NotificationRollup) (uint64, error) {
	for _, r := range f.rows {
		if r.Id == model.Id {
			*r = model
			return 1, nil
		}
	}
	return 0, nil
}

// count returns the stored count for a bucket, or 0 if the bucket is absent.
func (f *fakeRollupRepo) count(bucketStart, cameraID, ruleID int64, category, severity, label string) int64 {
	for _, r := range f.rows {
		if r.BucketStart == bucketStart && r.CameraId == cameraID && r.RuleId == ruleID &&
			r.Category == category && r.Severity == severity && r.Label == label {
			return r.Count
		}
	}
	return 0
}

func (f *fakeRollupRepo) totalCount() int64 {
	var sum int64
	for _, r := range f.rows {
		sum += r.Count
	}
	return sum
}

type fakeCursor struct{ id int64 }

func (c *fakeCursor) Get(context.Context) (int64, error)   { return c.id, nil }
func (c *fakeCursor) Set(_ context.Context, v int64) error { c.id = v; return nil }

func TestRollupMaintainerBackfillAndIncremental(t *testing.T) {
	// Hour-aligned base so bucket starts are predictable (1699920000 % 3600 == 0).
	const base = int64(1_699_920_000)
	const hour = int64(3600)

	notifs := &fakeNotifRepo{rows: []*entities.Notification{
		{Id: 1, CreatedAt: base + 10, Category: string(CategoryVisionAlert), Severity: "critical", CameraId: 1, Metadata: `{"ruleId":3,"label":"person"}`},
		{Id: 2, CreatedAt: base + 20, Category: string(CategoryVisionAlert), Severity: "critical", CameraId: 1, Metadata: `{"ruleId":3,"objectLabel":"person"}`},
		{Id: 3, CreatedAt: base + 30, Category: string(CategoryVisionAlert), Severity: "critical", CameraId: 1, Metadata: `{"ruleId":3,"objectLabel":"car"}`},
		{Id: 4, CreatedAt: base + hour + 5, Category: string(CategoryHealthCheck), Severity: "warning", CameraId: 2},
		{Id: 5, CreatedAt: base + hour + 9, Category: string(CategorySystem), Severity: "info"},
		{Id: 6, CreatedAt: base + 40, Category: string(CategoryVisionAlert), Severity: "critical", CameraId: 1, Metadata: `{"ruleId":5,"objectLabel":"person"}`},
	}}
	rollups := &fakeRollupRepo{}
	cursor := &fakeCursor{}
	// Request a large page size on purpose: the repo caps every query at a small
	// number regardless (emulated by fakeNotifRepo), so Sweep must drain via the
	// empty-page signal, not by comparing the page length to the requested size.
	m := NewRollupMaintainer(notifs, rollups, cursor, time.Second, 5000)

	// First sweep = full backfill.
	processed, err := m.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if processed != 6 {
		t.Errorf("processed = %d, want 6", processed)
	}
	if cursor.id != 6 {
		t.Errorf("cursor = %d, want 6", cursor.id)
	}
	if len(rollups.rows) != 5 {
		t.Fatalf("rollup rows = %d, want 5 distinct buckets", len(rollups.rows))
	}
	if rollups.totalCount() != 6 {
		t.Errorf("total rollup count = %d, want 6 (reconciles with notification count)", rollups.totalCount())
	}
	// The two person/rule-3 detections (via label + objectLabel) merge into one bucket.
	if got := rollups.count(base, 1, 3, string(CategoryVisionAlert), "critical", "person"); got != 2 {
		t.Errorf("person/rule3 bucket = %d, want 2", got)
	}
	if got := rollups.count(base, 1, 3, string(CategoryVisionAlert), "critical", "car"); got != 1 {
		t.Errorf("car/rule3 bucket = %d, want 1", got)
	}
	if got := rollups.count(base, 1, 5, string(CategoryVisionAlert), "critical", "person"); got != 1 {
		t.Errorf("person/rule5 bucket = %d, want 1 (distinct rule id)", got)
	}
	if got := rollups.count(base+hour, 2, 0, string(CategoryHealthCheck), "warning", ""); got != 1 {
		t.Errorf("health bucket = %d, want 1", got)
	}
	if got := rollups.count(base+hour, 0, 0, string(CategorySystem), "info", ""); got != 1 {
		t.Errorf("system bucket = %d, want 1", got)
	}

	// A second sweep with no new rows is a no-op (idempotent).
	if processed, _ := m.Sweep(context.Background()); processed != 0 {
		t.Errorf("re-sweep processed = %d, want 0", processed)
	}
	if rollups.totalCount() != 6 {
		t.Errorf("total after no-op sweep = %d, want 6", rollups.totalCount())
	}

	// Add new notifications and sweep incrementally — only the new rows fold in.
	notifs.rows = append(notifs.rows,
		&entities.Notification{Id: 7, CreatedAt: base + 50, Category: string(CategoryVisionAlert), Severity: "critical", CameraId: 1, Metadata: `{"ruleId":3,"objectLabel":"person"}`},
		&entities.Notification{Id: 8, CreatedAt: base + 2*hour + 5, Category: string(CategorySystem), Severity: "info"},
	)
	processed, err = m.Sweep(context.Background())
	if err != nil {
		t.Fatalf("incremental Sweep: %v", err)
	}
	if processed != 2 {
		t.Errorf("incremental processed = %d, want 2", processed)
	}
	if cursor.id != 8 {
		t.Errorf("cursor = %d, want 8", cursor.id)
	}
	if got := rollups.count(base, 1, 3, string(CategoryVisionAlert), "critical", "person"); got != 3 {
		t.Errorf("person/rule3 bucket after increment = %d, want 3", got)
	}
	if rollups.totalCount() != 8 {
		t.Errorf("total after increment = %d, want 8", rollups.totalCount())
	}
	if len(rollups.rows) != 6 {
		t.Errorf("rollup rows after increment = %d, want 6 (one new hour bucket)", len(rollups.rows))
	}
}
