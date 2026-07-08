package notification

import (
	"context"
	"math"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
)

// Date anchors (UTC, tzOffset 0 throughout so local == UTC).
// 2026-01-04 00:00 UTC is a Sunday; +1 day Monday, +2 Tuesday.
const (
	sunday0 = int64(1767484800)
	dayS    = int64(86400)
	weekS   = int64(7 * 86400)
)

func rollupAt(id, bucketStart, count int64) *entities.NotificationRollup {
	return &entities.NotificationRollup{Id: id, BucketStart: bucketStart, Category: string(CategoryVisionAlert), Severity: "critical", Count: count}
}

func TestBaselineRobustToOutlier(t *testing.T) {
	// Four Sundays with daily totals 10, 12, 11, and a 50 spike. A robust center
	// must sit near ~11 (not be dragged toward ~20 as a mean would), and the 50 must
	// land well above Hi so the frontend can flag it as a breach.
	rollups := &fakeRollupRepo{rows: []*entities.NotificationRollup{
		rollupAt(1, sunday0+12*3600, 10),
		rollupAt(2, sunday0+weekS+12*3600, 12),
		rollupAt(3, sunday0+2*weekS+12*3600, 11),
		rollupAt(4, sunday0+3*weekS+12*3600, 50),
	}}
	s := &Service{rollups: rollups}

	// Chart window = just the 4th Sunday (one daily bucket).
	from := sunday0 + 3*weekS
	to := from + dayS
	b, err := s.Baseline(context.Background(), from, to, dayS, 0, 0)
	if err != nil {
		t.Fatalf("Baseline: %v", err)
	}
	if len(b.Buckets) != 1 {
		t.Fatalf("buckets = %d, want 1", len(b.Buckets))
	}
	bb := b.Buckets[0]
	if bb.Learning {
		t.Fatalf("bucket unexpectedly learning (samples=%d)", bb.Samples)
	}
	if bb.Samples != 4 {
		t.Errorf("samples = %d, want 4", bb.Samples)
	}
	if bb.Mid < 10 || bb.Mid > 13 {
		t.Errorf("Mid = %.2f, want robust ~11.5 (not pulled toward the 50 outlier)", bb.Mid)
	}
	if bb.Hi >= 50 {
		t.Errorf("Hi = %.2f, want well below the 50 spike so it reads as a breach", bb.Hi)
	}
	if bb.Lo < 0 {
		t.Errorf("Lo = %.2f, want clamped >= 0", bb.Lo)
	}
}

func TestBaselinePoissonFloorAndColdStart(t *testing.T) {
	// Three identical Mondays (MAD collapses to 0 → Poisson floor) plus a single
	// Tuesday (too few samples → learning).
	monday0 := sunday0 + dayS
	tuesday0 := sunday0 + 2*dayS
	rollups := &fakeRollupRepo{rows: []*entities.NotificationRollup{
		rollupAt(1, monday0+9*3600, 4),
		rollupAt(2, monday0+weekS+9*3600, 4),
		rollupAt(3, monday0+2*weekS+9*3600, 4),
		rollupAt(4, tuesday0+9*3600, 5),
	}}
	s := &Service{rollups: rollups}

	// A Monday bucket: MAD is 0, so the band must fall back to a Poisson σ (√4 = 2)
	// rather than collapsing to a zero-width band that flags any deviation.
	mFrom := monday0 + 2*weekS
	mon, err := s.Baseline(context.Background(), mFrom, mFrom+dayS, dayS, 0, 0)
	if err != nil {
		t.Fatalf("Baseline monday: %v", err)
	}
	mb := mon.Buckets[0]
	if mb.Learning {
		t.Fatalf("monday unexpectedly learning")
	}
	if math.Abs(mb.Mid-4) > 0.01 {
		t.Errorf("Mid = %.2f, want 4", mb.Mid)
	}
	if mb.Hi <= 4.5 { // 4 + 3*√4 ≈ 10; must be clearly > mid, not a razor band
		t.Errorf("Hi = %.2f, want a Poisson-floored band (~10), not a collapsed one", mb.Hi)
	}

	// A Tuesday bucket has only one historical sample → learning, no band.
	tFrom := tuesday0
	tue, err := s.Baseline(context.Background(), tFrom, tFrom+dayS, dayS, 0, 0)
	if err != nil {
		t.Fatalf("Baseline tuesday: %v", err)
	}
	tb := tue.Buckets[0]
	if !tb.Learning {
		t.Errorf("tuesday should be learning (samples=%d)", tb.Samples)
	}
}
