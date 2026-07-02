package notification

import (
	"context"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fakeStatsRepo returns a fixed row set for Get and pages it, ignoring the
// filters (the Stats aggregation re-filters the window in Go). The embedded
// interface satisfies the rest of IGenericRepo; those methods are unused here.
type fakeStatsRepo struct {
	dbsql.IGenericRepo[entities.Notification]
	rows []*entities.Notification
}

func (f *fakeStatsRepo) Get(_ context.Context, _ string, limit, offset uint64, _ []sqldataenums.Filter, _ []sqldataenums.Sorter) ([]*entities.Notification, uint64, error) {
	total := uint64(len(f.rows))
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return f.rows[offset:end], total, nil
}

func TestStatsAggregatesWindow(t *testing.T) {
	// Day-aligned start (divisible by 86400) so the 3-day window yields exactly 3
	// whole-day buckets — matching how the UI anchors ranges to local midnight.
	const from = int64(1_699_920_000)
	const day = int64(86400)
	to := from + 3*day

	rows := []*entities.Notification{
		{CreatedAt: from - 100, Category: string(CategorySystem), Severity: "info", Source: "local-auth"}, // previous window
		{CreatedAt: from + 10, Category: string(CategoryVisionAlert), Severity: "critical", Source: "vision-monitor", CameraId: 1, Metadata: `{"label":"person"}`},
		{CreatedAt: from + 20, Category: string(CategoryVisionAlert), Severity: "critical", Source: "vision-monitor", CameraId: 1, Metadata: `{"objectLabel":"car"}`},
		{CreatedAt: from + day + 5, Category: string(CategoryHealthCheck), Severity: "warning", Source: "camera-health-monitor", CameraId: 2},
		{CreatedAt: from + 2*day + 5, Category: string(CategorySystem), Severity: "info", Source: "local-auth", IsRead: true},
	}
	s := &Service{repo: &fakeStatsRepo{rows: rows}}

	got, err := s.Stats(context.Background(), from, to, day, 0)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if got.Total != 4 {
		t.Errorf("Total = %d, want 4", got.Total)
	}
	if got.PrevTotal != 1 {
		t.Errorf("PrevTotal = %d, want 1", got.PrevTotal)
	}
	if got.Critical != 2 || got.Warning != 1 || got.Info != 1 {
		t.Errorf("severity split = c%d w%d i%d, want c2 w1 i1", got.Critical, got.Warning, got.Info)
	}
	if got.Unread != 3 {
		t.Errorf("Unread = %d, want 3 (the read system row excluded)", got.Unread)
	}
	if got.Bucket != "day" {
		t.Errorf("Bucket = %q, want day", got.Bucket)
	}

	if len(got.ByCategory) != 3 || got.ByCategory[0].Key != string(CategoryVisionAlert) || got.ByCategory[0].Count != 2 {
		t.Errorf("ByCategory top = %+v, want vision.alert:2 first", got.ByCategory)
	}

	if len(got.TopCameras) != 2 || got.TopCameras[0].CameraId != 1 || got.TopCameras[0].Count != 2 {
		t.Errorf("TopCameras = %+v, want camera 1 with 2 first", got.TopCameras)
	}

	// Two distinct labels, each once.
	labels := map[string]int64{}
	for _, l := range got.TopLabels {
		labels[l.Key] = l.Count
	}
	if labels["person"] != 1 || labels["car"] != 1 {
		t.Errorf("TopLabels = %+v, want person:1 car:1", got.TopLabels)
	}

	// Three day buckets, with the first holding the two same-day detections.
	if len(got.Timeseries) != 3 {
		t.Fatalf("Timeseries len = %d, want 3", len(got.Timeseries))
	}
	if got.Timeseries[0].Total != 2 || got.Timeseries[0].ByCategory[string(CategoryVisionAlert)] != 2 {
		t.Errorf("bucket0 = %+v, want total 2 / vision.alert 2", got.Timeseries[0])
	}
	if got.Timeseries[1].Total != 1 || got.Timeseries[2].Total != 1 {
		t.Errorf("bucket totals = %d,%d, want 1,1", got.Timeseries[1].Total, got.Timeseries[2].Total)
	}
}

func TestBucketStartAlignsToLocalDay(t *testing.T) {
	// UTC+8 (28800s): a timestamp at 2026-01-01 01:00 local should bucket to the
	// local midnight, i.e. 2025-12-31 16:00 UTC.
	const tz = int64(8 * 3600)
	const day = int64(86400)
	// 2026-01-01 00:00:00 UTC = 1767225600; local midnight in UTC+8 is 8h earlier.
	utcMidnightPlus1h := int64(1767225600) + 3600 - tz // 01:00 local, expressed in UTC
	start := bucketStart(utcMidnightPlus1h, day, tz)
	wantStart := int64(1767225600) - tz // local midnight expressed in UTC
	if start != wantStart {
		t.Errorf("bucketStart = %d, want %d", start, wantStart)
	}
}
