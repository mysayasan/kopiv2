package notification

import (
	"context"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
)

func TestAnomalyScanFindsSpikeAndSilence(t *testing.T) {
	// All rows land on Mondays at 14:00 (a single weekday+hour slot). The 4th Monday
	// is the hour under test.
	monday14 := sunday0 + dayS + 14*3600
	target := monday14 + 3*weekS

	var id int64
	add := func(rows *[]*entities.NotificationRollup, cam, bucket, count int64) {
		id++
		*rows = append(*rows, &entities.NotificationRollup{
			Id: id, BucketStart: bucket, CameraId: cam,
			Category: string(CategoryVisionAlert), Severity: "critical", Count: count,
		})
	}
	var rows []*entities.NotificationRollup
	// cam1: steady 10s, then a 40 spike this hour → HIGH.
	add(&rows, 1, monday14, 10)
	add(&rows, 1, monday14+weekS, 10)
	add(&rows, 1, monday14+2*weekS, 10)
	add(&rows, 1, target, 40)
	// cam2: normally busy (20s), silent this hour (no target row) → LOW.
	add(&rows, 2, monday14, 20)
	add(&rows, 2, monday14+weekS, 20)
	add(&rows, 2, monday14+2*weekS, 20)
	// cam3: steady 15s including this hour → within band, no finding.
	add(&rows, 3, monday14, 15)
	add(&rows, 3, monday14+weekS, 15)
	add(&rows, 3, monday14+2*weekS, 15)
	add(&rows, 3, target, 15)
	// cam4: normally near-silent (2s), silent this hour → suppressed by minActivity.
	add(&rows, 4, monday14, 2)
	add(&rows, 4, monday14+weekS, 2)
	add(&rows, 4, monday14+2*weekS, 2)
	// cam5: only one prior sample → cold start, skipped.
	add(&rows, 5, monday14, 30)

	s := &Service{rollups: &fakeRollupRepo{rows: rows}}
	findings, err := s.AnomalyScan(context.Background(), target, 0, 3.0, 3.0)
	if err != nil {
		t.Fatalf("AnomalyScan: %v", err)
	}

	byCam := map[int64]AnomalyFinding{}
	for _, f := range findings {
		byCam[f.CameraId] = f
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %d (%+v), want 2 (cam1 high, cam2 low)", len(findings), findings)
	}
	if f, ok := byCam[1]; !ok || f.Direction != AnomalyHigh || f.Actual != 40 {
		t.Errorf("cam1 = %+v, want high spike actual 40", byCam[1])
	}
	if f, ok := byCam[2]; !ok || f.Direction != AnomalyLow || f.Actual != 0 {
		t.Errorf("cam2 = %+v, want low silence actual 0", byCam[2])
	}
	if _, ok := byCam[3]; ok {
		t.Errorf("cam3 should be within band (no finding), got %+v", byCam[3])
	}
	if _, ok := byCam[4]; ok {
		t.Errorf("cam4 silence should be suppressed by minActivity, got %+v", byCam[4])
	}
	if _, ok := byCam[5]; ok {
		t.Errorf("cam5 should be skipped (cold start), got %+v", byCam[5])
	}
}

func TestAnomalyScanRespectsMinActivityFloorForSilence(t *testing.T) {
	// A slot whose normal median is exactly at the floor should still allow a silence
	// finding; below the floor it must not.
	monday14 := sunday0 + dayS + 14*3600
	target := monday14 + 3*weekS
	rows := []*entities.NotificationRollup{
		{Id: 1, BucketStart: monday14, CameraId: 9, Count: 5},
		{Id: 2, BucketStart: monday14 + weekS, CameraId: 9, Count: 5},
		{Id: 3, BucketStart: monday14 + 2*weekS, CameraId: 9, Count: 5},
	}
	s := &Service{rollups: &fakeRollupRepo{rows: rows}}

	// minActivity 5 → median 5 qualifies → silence flagged.
	if f, _ := s.AnomalyScan(context.Background(), target, 0, 3.0, 5.0); len(f) != 1 || f[0].Direction != AnomalyLow {
		t.Errorf("minActivity 5: got %+v, want one low finding", f)
	}
	// minActivity 6 → median 5 below floor → suppressed.
	if f, _ := s.AnomalyScan(context.Background(), target, 0, 3.0, 6.0); len(f) != 0 {
		t.Errorf("minActivity 6: got %+v, want no findings", f)
	}
}
