package services

import (
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
)

// The hour under test: 2026-08-19 14:00–15:00 UTC.
const (
	testHourStart = int64(1755612000)
	testHourEnd   = testHourStart + 3600
)

func seg(start, end int64) *entities.RecordingSegment {
	return &entities.RecordingSegment{CameraId: 3, StartedAt: start, EndedAt: end}
}

func TestCoverageFullHour(t *testing.T) {
	// Four fifteen-minute segments, back to back.
	segs := []*entities.RecordingSegment{
		seg(testHourStart, testHourStart+900),
		seg(testHourStart+900, testHourStart+1800),
		seg(testHourStart+1800, testHourStart+2700),
		seg(testHourStart+2700, testHourEnd),
	}
	got, n := coveredSeconds(segs, testHourStart, testHourEnd)
	if got != 3600 {
		t.Fatalf("covered = %d, want 3600", got)
	}
	if n != 4 {
		t.Fatalf("segments counted = %d, want 4", n)
	}
}

func TestCoveragePartialHourReportsTheHole(t *testing.T) {
	// Twenty minutes recorded, forty missing — the shape of a wedged ffmpeg.
	segs := []*entities.RecordingSegment{seg(testHourStart, testHourStart+1200)}
	got, _ := coveredSeconds(segs, testHourStart, testHourEnd)
	if got != 1200 {
		t.Fatalf("covered = %d, want 1200", got)
	}
}

func TestCoverageEmptyHourIsZero(t *testing.T) {
	got, n := coveredSeconds(nil, testHourStart, testHourEnd)
	if got != 0 || n != 0 {
		t.Fatalf("covered = %d / %d segments, want 0 / 0", got, n)
	}
}

// Overlap is normal, not exceptional: a recorder restart re-opens a segment overlapping
// the previous one, and an event clip can be cut from the same footage as a continuous
// segment. Summing durations without merging would report MORE than 100% for an hour that
// actually has a hole in it — which is worse than reporting nothing, because it reads as
// proof the footage is there.
func TestCoverageDoesNotDoubleCountOverlaps(t *testing.T) {
	segs := []*entities.RecordingSegment{
		seg(testHourStart, testHourStart+1800),      // 0–30
		seg(testHourStart+900, testHourStart+2700),  // 15–45, overlaps both
		seg(testHourStart+1800, testHourStart+2700), // 30–45
	}
	got, _ := coveredSeconds(segs, testHourStart, testHourEnd)
	if got != 2700 {
		t.Fatalf("covered = %d, want 2700 (0–45 min merged, not summed)", got)
	}
	if got > 3600 {
		t.Fatal("coverage exceeded the window — overlaps were summed")
	}
}

// A segment that STARTED before the hour and runs into it covers the first minutes of
// that hour. Missing it would under-report exactly the boundary where a recorder rolls
// over, and make every hour look slightly short.
func TestCoverageCountsASegmentStraddlingTheStart(t *testing.T) {
	segs := []*entities.RecordingSegment{
		seg(testHourStart-600, testHourStart+600), // starts 10 min before, ends 10 min in
	}
	got, _ := coveredSeconds(segs, testHourStart, testHourEnd)
	if got != 600 {
		t.Fatalf("covered = %d, want 600 — only the in-window part counts", got)
	}
}

func TestCoverageClipsASegmentRunningPastTheEnd(t *testing.T) {
	segs := []*entities.RecordingSegment{seg(testHourEnd-300, testHourEnd+900)}
	got, _ := coveredSeconds(segs, testHourStart, testHourEnd)
	if got != 300 {
		t.Fatalf("covered = %d, want 300 — the part past the window must not count", got)
	}
}

func TestCoverageIgnoresSegmentsEntirelyOutsideTheWindow(t *testing.T) {
	segs := []*entities.RecordingSegment{
		seg(testHourStart-7200, testHourStart-3600),
		seg(testHourEnd+3600, testHourEnd+7200),
	}
	got, n := coveredSeconds(segs, testHourStart, testHourEnd)
	if got != 0 || n != 0 {
		t.Fatalf("covered = %d / %d segments, want 0 / 0", got, n)
	}
}

// A segment still being written has no EndedAt yet. Treating it as zero-length would
// under-report the current hour and raise a false gap; treating it as open-ended would
// credit a crashed recorder's abandoned row with footage that does not exist. Clipping to
// the window end credits only time that has actually elapsed.
func TestCoverageTreatsAnUnfinishedSegmentAsRunningToTheWindowEnd(t *testing.T) {
	segs := []*entities.RecordingSegment{seg(testHourStart+1800, 0)}
	got, _ := coveredSeconds(segs, testHourStart, testHourEnd)
	if got != 1800 {
		t.Fatalf("covered = %d, want 1800", got)
	}
}

func TestCoverageZeroLengthWindowIsZero(t *testing.T) {
	if got, _ := coveredSeconds([]*entities.RecordingSegment{seg(0, 100)}, 500, 500); got != 0 {
		t.Fatalf("covered = %d, want 0", got)
	}
}

func TestMergeIntervalsCoalescesTouchingSpans(t *testing.T) {
	// Touching (end == next start) must merge, or every segment boundary would read as
	// a one-second hole and no camera would ever reach 100%.
	got := mergeIntervals([]interval{{0, 10}, {10, 20}, {30, 40}})
	if len(got) != 2 {
		t.Fatalf("merged into %d spans, want 2: %+v", len(got), got)
	}
	if got[0].start != 0 || got[0].end != 20 {
		t.Fatalf("first span = %+v, want {0 20}", got[0])
	}
}
