package services

import (
	"context"
	"sort"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
)

// Recording coverage: how much of a window actually has footage on disk.
//
// This is the number an NVR is bought for and the one nothing in the product could
// answer. The camera health monitor probes reachability, which is a different question:
// a camera can be perfectly reachable while ffmpeg is wedged, the disk is full, the
// remux queue is quarantining every segment, or the stream URL silently changed. All of
// those record nothing and report green.

// coverageLookbackSlack widens the segment query backwards past the window start.
//
// GetSegments filters on StartedAt, so a segment that began BEFORE the window and runs
// into it would be missed by an exact-window query — and that segment is usually the one
// covering the first minutes of the hour. The slack has to exceed the longest plausible
// segment; segments are configured in minutes and default well under an hour, so an hour
// is generous without pulling in a meaningful amount of extra data.
const coverageLookbackSlack = int64(time.Hour / time.Second)

// CoverageBucket is one scored slice of time.
type CoverageBucket struct {
	// From/To bound the bucket (unix seconds), To exclusive.
	From int64 `json:"from"`
	To   int64 `json:"to"`
	// CoveredSeconds is how much of the bucket has footage, after merging overlaps.
	CoveredSeconds int64 `json:"coveredSeconds"`
	// Percent is CoveredSeconds as a percentage of the bucket's length.
	Percent float64 `json:"percent"`
	// Segments is how many segment rows contributed.
	Segments int `json:"segments"`
}

// CoverageReport is a camera's coverage across a requested range.
type CoverageReport struct {
	CameraId int64            `json:"cameraId"`
	From     int64            `json:"from"`
	To       int64            `json:"to"`
	Bucket   string           `json:"bucket"`
	Buckets  []CoverageBucket `json:"buckets"`
	// OverallPercent is coverage across the whole range, not the mean of the buckets —
	// averaging percentages would weight a part-elapsed final bucket the same as a full one.
	OverallPercent float64 `json:"overallPercent"`
}

// interval is a half-open [start, end) span in unix seconds.
type interval struct{ start, end int64 }

// mergeIntervals sorts and coalesces overlapping spans.
//
// Overlap is normal rather than exceptional: a recorder restart re-opens a segment that
// overlaps the previous one, and an event clip can be extracted from the same footage as
// a continuous segment. Summing durations without merging would report more than 100%
// coverage for an hour that actually has a hole in it — which is worse than reporting
// nothing, because it reads as proof the footage is there.
func mergeIntervals(in []interval) []interval {
	if len(in) == 0 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool {
		if in[i].start != in[j].start {
			return in[i].start < in[j].start
		}
		return in[i].end < in[j].end
	})
	out := []interval{in[0]}
	for _, cur := range in[1:] {
		last := &out[len(out)-1]
		if cur.start <= last.end {
			if cur.end > last.end {
				last.end = cur.end
			}
			continue
		}
		out = append(out, cur)
	}
	return out
}

// coveredSeconds returns how much of [from, to) the segments cover, clipping each to the
// window and merging overlaps.
func coveredSeconds(segs []*entities.RecordingSegment, from, to int64) (int64, int) {
	if to <= from {
		return 0, 0
	}
	spans := make([]interval, 0, len(segs))
	counted := 0
	for _, s := range segs {
		if s == nil {
			continue
		}
		start, end := s.StartedAt, s.EndedAt
		// A segment still being written has no end yet. Treating it as zero-length would
		// under-report the current hour; treating it as open-ended would over-report a
		// crashed recorder's abandoned row. Clipping to the window end is the honest
		// middle: it credits only time that has actually elapsed.
		if end <= 0 {
			end = to
		}
		if start < from {
			start = from
		}
		if end > to {
			end = to
		}
		if end <= start {
			continue
		}
		spans = append(spans, interval{start: start, end: end})
		counted++
	}
	var total int64
	for _, sp := range mergeIntervals(spans) {
		total += sp.end - sp.start
	}
	return total, counted
}

// CoverageBucketSize maps a bucket name to its length.
func CoverageBucketSize(bucket string) time.Duration {
	if bucket == "day" {
		return 24 * time.Hour
	}
	return time.Hour
}

// CoverageBucketSeconds is CoverageBucketSize in seconds, for range-bound arithmetic.
func CoverageBucketSeconds(bucket string) int64 {
	return int64(CoverageBucketSize(bucket) / time.Second)
}

// Coverage computes a camera's recording coverage over [from, to), bucketed hourly or
// daily. It is the read model behind both the coverage screen and the continuity monitor.
func (s *recordingService) Coverage(ctx context.Context, cameraId, from, to int64, bucket string) (CoverageReport, error) {
	size := int64(CoverageBucketSize(bucket) / time.Second)
	report := CoverageReport{CameraId: cameraId, From: from, To: to, Bucket: bucket}
	if to <= from || cameraId <= 0 {
		return report, nil
	}

	segs, _, err := s.GetSegments(ctx, coverageQueryLimit, 0, cameraId, 0, from-coverageLookbackSlack, to)
	if err != nil {
		return report, err
	}

	for start := from; start < to; start += size {
		end := start + size
		if end > to {
			end = to
		}
		covered, n := coveredSeconds(segs, start, end)
		span := end - start
		pct := 0.0
		if span > 0 {
			pct = float64(covered) / float64(span) * 100
		}
		report.Buckets = append(report.Buckets, CoverageBucket{
			From: start, To: end, CoveredSeconds: covered, Percent: round2(pct), Segments: n,
		})
	}

	covered, _ := coveredSeconds(segs, from, to)
	report.OverallPercent = round2(float64(covered) / float64(to-from) * 100)
	return report, nil
}

// coverageQueryLimit bounds one coverage query. At the shortest sensible segment length
// this still spans several days for one camera, and the API caps the requested range.
const coverageQueryLimit = 20000

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
