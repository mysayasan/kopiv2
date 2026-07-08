package notification

import (
	"context"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
)

func cellCount(h *Heatmap, day, hour int) int64 {
	for _, c := range h.Cells {
		if c.Day == day && c.Hour == hour {
			return c.Count
		}
	}
	return 0
}

func TestHeatmapAggregatesLocalSlots(t *testing.T) {
	// 2026-01-01 00:00:00 UTC is a Thursday (Weekday 4, Sunday=0).
	const midnight = int64(1767225600)
	const hour = int64(3600)
	const thursday = 4

	rollups := &fakeRollupRepo{rows: []*entities.NotificationRollup{
		{Id: 1, BucketStart: midnight, CameraId: 1, Category: string(CategoryVisionAlert), Severity: "critical", Count: 5},
		{Id: 2, BucketStart: midnight + hour, CameraId: 1, Category: string(CategoryVisionAlert), Severity: "critical", Count: 3},
		{Id: 3, BucketStart: midnight, CameraId: 2, Category: string(CategoryHealthCheck), Severity: "warning", Count: 2},
	}}
	s := &Service{rollups: rollups}

	// All cameras, no tz shift: the two midnight buckets (cameras 1+2) merge into the
	// Thursday-00:00 cell; the 01:00 bucket lands in Thursday-01:00.
	all, err := s.Heatmap(context.Background(), midnight-hour, midnight+2*hour, 0, 0)
	if err != nil {
		t.Fatalf("Heatmap: %v", err)
	}
	if all.Total != 10 {
		t.Errorf("total = %d, want 10", all.Total)
	}
	if all.Max != 7 {
		t.Errorf("max = %d, want 7", all.Max)
	}
	if got := cellCount(all, thursday, 0); got != 7 {
		t.Errorf("Thu-00:00 = %d, want 7 (5+2 merged across cameras)", got)
	}
	if got := cellCount(all, thursday, 1); got != 3 {
		t.Errorf("Thu-01:00 = %d, want 3", got)
	}

	// Scope to camera 1: camera 2's contribution drops out.
	cam1, err := s.Heatmap(context.Background(), midnight-hour, midnight+2*hour, 1, 0)
	if err != nil {
		t.Fatalf("Heatmap cam1: %v", err)
	}
	if cam1.Total != 8 {
		t.Errorf("cam1 total = %d, want 8", cam1.Total)
	}
	if got := cellCount(cam1, thursday, 0); got != 5 {
		t.Errorf("cam1 Thu-00:00 = %d, want 5", got)
	}

	// UTC+1 shifts every bucket one hour later: the 00:00 UTC buckets become 01:00.
	shifted, err := s.Heatmap(context.Background(), midnight-hour, midnight+2*hour, 0, hour)
	if err != nil {
		t.Fatalf("Heatmap shifted: %v", err)
	}
	if got := cellCount(shifted, thursday, 1); got != 7 {
		t.Errorf("shifted Thu-01:00 = %d, want 7 (00:00 UTC + 1h)", got)
	}
	if got := cellCount(shifted, thursday, 2); got != 3 {
		t.Errorf("shifted Thu-02:00 = %d, want 3", got)
	}
}
