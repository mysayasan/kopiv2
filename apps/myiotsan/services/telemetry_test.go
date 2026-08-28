package services

import (
	"testing"

	"github.com/mysayasan/kopiv2/apps/myiotsan/entities"
)

// dropIncompleteTail is the guard on the rollup's batch boundary. It is pinned here rather than
// live because triggering it needs more raw readings in one interval than a bench can produce,
// and the consequence of getting it wrong is invisible: a bucket summarized from half its rows,
// which the cursor then steps past forever.

func at(secs int64) *entities.DeviceReading {
	return &entities.DeviceReading{Ts: secs * 1000}
}

func TestDropIncompleteTail_KeepsEverythingWhenTheReadWasNotTruncated(t *testing.T) {
	rows := []*entities.DeviceReading{at(0), at(30), at(61), at(65)}
	got := dropIncompleteTail(rows, 60, false)
	if len(got) != 4 {
		t.Fatalf("a read that did not fill its batch is complete by definition, kept %d of 4", len(got))
	}
}

func TestDropIncompleteTail_StopsAtTheLastWholeBucket(t *testing.T) {
	// Buckets 0 and 60. The batch ended inside bucket 60, so only bucket 0 is provably whole.
	rows := []*entities.DeviceReading{at(0), at(30), at(59), at(60), at(61)}
	got := dropIncompleteTail(rows, 60, true)
	if len(got) != 3 {
		t.Fatalf("expected the 3 rows of bucket 0, got %d", len(got))
	}
	for _, r := range got {
		if r.Ts/1000/60*60 != 0 {
			t.Fatalf("a row from bucket %d survived the cut", r.Ts/1000/60*60)
		}
	}
}

func TestDropIncompleteTail_KeepsABucketWiderThanTheWholeBatch(t *testing.T) {
	// Every row is in one bucket. Dropping them would leave nothing to fold, the cursor would
	// not advance, and the next pass would read and drop exactly the same rows — a rollup that
	// silently stops forever. A partial summary is the lesser harm.
	rows := []*entities.DeviceReading{at(10), at(20), at(30), at(40)}
	got := dropIncompleteTail(rows, 60, true)
	if len(got) != 4 {
		t.Fatalf("a single oversized bucket must be folded rather than stall the rollup, kept %d of 4", len(got))
	}
}

func TestDropIncompleteTail_EmptyInput(t *testing.T) {
	if got := dropIncompleteTail(nil, 60, true); len(got) != 0 {
		t.Fatalf("nil in, %d out", len(got))
	}
}

// The cutoff arithmetic itself: "now minus one width" is not a bucket boundary, and the whole
// point of the fix is that the cut lands on one. This pins the expression rather than the clock.
func TestRollupCutoffIsABucketBoundary(t *testing.T) {
	cases := []struct{ now, secs, want int64 }{
		{now: 90, secs: 60, want: 60},  // 10:01:30 cuts at 10:01:00, not 10:00:30
		{now: 60, secs: 60, want: 60},  // exactly on a boundary stays there
		{now: 119, secs: 60, want: 60}, // one second short of the next boundary
		{now: 7199, secs: 3600, want: 3600},
	}
	for _, c := range cases {
		if got := c.now / c.secs * c.secs; got != c.want {
			t.Fatalf("cutoff for now=%d span=%ds: got %d, want %d", c.now, c.secs, got, c.want)
		}
	}
}
