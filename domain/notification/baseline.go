package notification

import (
	"context"
	"math"
	"sort"
	"time"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// Baseline tuning. These are visual-band defaults; the Phase 3 anomaly monitor
// will expose k and the lookback as user settings.
const (
	baselineLookbackWeeks = 8   // how far back to gather per-slot samples
	baselineK             = 3.0 // band half-width in robust sigmas (conservative)
	baselineMinSamples    = 2   // fewer than this → "learning", no band
	madToSigma            = 1.4826
)

// BaselineBucket is the expected activity band for one timeseries bucket, aligned
// 1:1 with the events-over-time chart's buckets. Lo/Hi bound the "normal" range;
// Mid is the robust center (median). Learning is true when the slot has too few
// historical samples to trust a band yet (Lo/Hi/Mid are then zero).
type BaselineBucket struct {
	Start    int64   `json:"start"`
	Mid      float64 `json:"mid"`
	Lo       float64 `json:"lo"`
	Hi       float64 `json:"hi"`
	Samples  int     `json:"samples"`
	Learning bool    `json:"learning"`
}

// Baseline is the expected-activity envelope for a chart window: for each bucket,
// the normal range of total events derived from the same slot (hour-of-day for an
// hourly chart, day-of-week for a daily chart) over the trailing weeks. It is the
// "expected band" drawn behind the events-over-time line, and the statistical core
// the anomaly alerts (Phase 3) will reuse.
type Baseline struct {
	From          int64            `json:"from"`
	To            int64            `json:"to"`
	Bucket        string           `json:"bucket"`
	K             float64          `json:"k"`
	LookbackWeeks int              `json:"lookbackWeeks"`
	Buckets       []BaselineBucket `json:"buckets"`
}

// Baseline computes the expected-activity band for each bucket in [from, to],
// matching the chart's granularity (bucketSeconds 3600 = hour-of-day slots, 86400
// = day-of-week slots). Samples for a slot are the per-occurrence event totals over
// the trailing baselineLookbackWeeks, aggregated from the rollup and scoped to one
// camera when cameraId > 0. The band is robust (median ± k·1.4826·MAD) with a
// Poisson-style floor for sparse slots where MAD collapses to zero, so quiet hours
// don't produce a razor-thin band that flags every nonzero count.
// source, when non-empty, narrows the band to one notification source (e.g.
// "node:<id>" on a control plane) — the per-node baseline. "" = all sources
// summed, which includes rows folded before the source dimension existed, so
// fleet-wide bands keep their full history across the upgrade.
func (s *Service) Baseline(ctx context.Context, from, to, bucketSeconds, tzOffsetSec, cameraId int64, source string) (*Baseline, error) {
	out := &Baseline{From: from, To: to, K: baselineK, LookbackWeeks: baselineLookbackWeeks}
	if bucketSeconds == 3600 {
		out.Bucket = "hour"
	} else {
		out.Bucket = "day"
		bucketSeconds = 86400
	}
	if s.rollups == nil || to <= 0 {
		return out, nil
	}

	histFrom := to - int64(baselineLookbackWeeks)*7*86400
	if histFrom < 0 {
		histFrom = 0
	}

	filters := []sqldataenums.Filter{
		{FieldName: "BucketStart", Compare: sqldataenums.GreaterThanOrEqualTo, Value: histFrom},
		{FieldName: "BucketStart", Compare: sqldataenums.LessThanOrEqualTo, Value: to},
	}
	if cameraId > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId})
	}
	if source != "" {
		filters = append(filters, sqldataenums.Filter{FieldName: "Source", Compare: sqldataenums.Equal, Value: source})
	}

	// Collapse the rollup to one total per occurrence (a distinct local hour or day),
	// summing across cameras/categories/labels, so a slot's samples are whole-window
	// totals rather than per-dimension fragments.
	occTotals := map[int64]int64{}
	var offset uint64
	for {
		page, total, err := s.rollups.Get(ctx, "", statsPageSize, offset, filters, nil)
		if err != nil {
			return nil, err
		}
		for _, row := range page {
			occTotals[occurrenceKey(row.BucketStart, bucketSeconds, tzOffsetSec)] += row.Count
		}
		offset += uint64(len(page))
		if len(page) == 0 || offset >= total || offset >= statsMaxRows {
			break
		}
	}

	// Bucket occurrence totals into their slot (hour-of-day or day-of-week).
	samplesBySlot := map[int]([]float64){}
	for occStart, sum := range occTotals {
		slot := slotKey(occStart, tzOffsetSec, out.Bucket)
		samplesBySlot[slot] = append(samplesBySlot[slot], float64(sum))
	}

	// Emit a band per chart bucket, aligned to the same boundaries the timeseries uses.
	first := bucketStart(from, bucketSeconds, tzOffsetSec)
	for start := first; start < to; start += bucketSeconds {
		slot := slotKey(start, tzOffsetSec, out.Bucket)
		samples := samplesBySlot[slot]
		bb := BaselineBucket{Start: start, Samples: len(samples)}
		if len(samples) < baselineMinSamples {
			bb.Learning = true
		} else {
			mid, lo, hi := robustBand(samples, baselineK)
			bb.Mid, bb.Lo, bb.Hi = mid, lo, hi
		}
		out.Buckets = append(out.Buckets, bb)
	}
	return out, nil
}

// occurrenceKey aligns a UTC bucket start to the local hour or day it falls in, so
// distinct occurrences (each 2pm, or each Monday) can be summed independently.
func occurrenceKey(bucketStartUTC, bucketSeconds, tzOffsetSec int64) int64 {
	local := bucketStartUTC + tzOffsetSec
	return local - ((local%bucketSeconds)+bucketSeconds)%bucketSeconds
}

// slotKey returns the seasonal slot a bucket belongs to: hour-of-day (0..23) for an
// hourly chart, or day-of-week (0=Sunday..6) for a daily chart.
func slotKey(bucketStartUTC, tzOffsetSec int64, bucket string) int {
	t := time.Unix(bucketStartUTC+tzOffsetSec, 0).UTC()
	if bucket == "hour" {
		return int(t.Weekday())*24 + t.Hour()
	}
	return int(t.Weekday())
}

// robustBand returns the median center and the [lo, hi] normal range for a set of
// samples. Half-width is k · 1.4826 · MAD (a robust σ estimate); when MAD is zero
// (a slot that is almost always the same low count) it falls back to a Poisson σ
// (√mean, floored at 1) so the band stays sane instead of collapsing. Lo is clamped
// at zero — negative event counts are meaningless.
func robustBand(samples []float64, k float64) (mid, lo, hi float64) {
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	mid = median(sorted)

	deviations := make([]float64, len(sorted))
	var sum float64
	for i, v := range sorted {
		deviations[i] = math.Abs(v - mid)
		sum += v
	}
	sort.Float64s(deviations)
	sigma := madToSigma * median(deviations)
	if sigma < 0.5 {
		mean := sum / float64(len(sorted))
		sigma = math.Sqrt(math.Max(mean, 1))
	}
	half := k * sigma
	lo = math.Max(0, mid-half)
	hi = mid + half
	return mid, lo, hi
}

// median returns the median of a pre-sorted slice (0 for empty).
func median(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
