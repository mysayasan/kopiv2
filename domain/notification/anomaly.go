package notification

import (
	"context"
	"sort"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// AnomalyFinding is one camera whose activity in a closed hour fell outside its
// learned normal range for that weekday+hour slot. Direction is "high" (a spike)
// or "low" (unusual silence — quieter than a normally-active slot should be).
type AnomalyFinding struct {
	CameraId  int64   `json:"cameraId"`
	Direction string  `json:"direction"`
	HourStart int64   `json:"hourStart"`
	Actual    int64   `json:"actual"`
	Mid       float64 `json:"mid"`
	Lo        float64 `json:"lo"`
	Hi        float64 `json:"hi"`
	Samples   int     `json:"samples"`
}

// Anomaly directions.
const (
	AnomalyHigh = "high"
	AnomalyLow  = "low"
)

// AnomalyScan evaluates one closed hour bucket, per camera, against that camera's
// own (weekday, hour) baseline built from the trailing weeks of rollup history. It
// returns cameras whose count is above the expected high (a spike) or below the
// expected low while the slot is normally active (unusual silence — the tamper /
// obstruction / offline signal). k sets the band width; minActivity suppresses
// silence findings for slots that are normally quiet, so 3am silence is not flagged
// as "unusual". Cameras with too little history for the slot are skipped (learning).
func (s *Service) AnomalyScan(ctx context.Context, hourStart, tzOffsetSec int64, k, minActivity float64) ([]AnomalyFinding, error) {
	if s.rollups == nil || hourStart <= 0 {
		return nil, nil
	}
	hourStart -= hourStart % 3600
	hourEnd := hourStart + 3600
	histFrom := hourStart - int64(baselineLookbackWeeks)*7*86400
	if histFrom < 0 {
		histFrom = 0
	}

	filters := []sqldataenums.Filter{
		{FieldName: "BucketStart", Compare: sqldataenums.GreaterThanOrEqualTo, Value: histFrom},
		{FieldName: "BucketStart", Compare: sqldataenums.LessThanOrEqualTo, Value: hourEnd},
	}

	// Per camera, collapse the rollup to a total per local-hour occurrence.
	perCamera := map[int64]map[int64]int64{}
	var offset uint64
	for {
		page, total, err := s.rollups.Get(ctx, "", statsPageSize, offset, filters, nil)
		if err != nil {
			return nil, err
		}
		for _, row := range page {
			occ := occurrenceKey(row.BucketStart, 3600, tzOffsetSec)
			m := perCamera[row.CameraId]
			if m == nil {
				m = map[int64]int64{}
				perCamera[row.CameraId] = m
			}
			m[occ] += row.Count
		}
		offset += uint64(len(page))
		if len(page) == 0 || offset >= total || offset >= statsMaxRows {
			break
		}
	}

	targetOcc := occurrenceKey(hourStart, 3600, tzOffsetSec)
	targetSlot := slotKey(targetOcc, 0, "hour")

	var findings []AnomalyFinding
	for cameraId, occs := range perCamera {
		samples := make([]float64, 0, len(occs))
		for occ, sum := range occs {
			if slotKey(occ, 0, "hour") == targetSlot {
				samples = append(samples, float64(sum))
			}
		}
		if len(samples) < baselineMinSamples {
			continue
		}
		mid, lo, hi := robustBand(samples, k)
		actual := occs[targetOcc]
		// "Unusual silence" fires when a normally-active slot (median ≥ minActivity)
		// drops below its lower band OR goes completely dark. The explicit zero case
		// matters because for low-count slots the robust lower bound underflows and
		// clamps to 0, so a strict `actual < lo` would never catch a camera that went
		// from "some activity" to nothing — the exact tamper/obstruction/offline signal.
		switch {
		case float64(actual) > hi:
			findings = append(findings, AnomalyFinding{CameraId: cameraId, Direction: AnomalyHigh, HourStart: hourStart, Actual: actual, Mid: mid, Lo: lo, Hi: hi, Samples: len(samples)})
		case mid >= minActivity && (float64(actual) < lo || actual == 0):
			findings = append(findings, AnomalyFinding{CameraId: cameraId, Direction: AnomalyLow, HourStart: hourStart, Actual: actual, Mid: mid, Lo: lo, Hi: hi, Samples: len(samples)})
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].CameraId < findings[j].CameraId })
	return findings, nil
}

// ManualScan is the non-statistical "manual thresholds" tier: it compares the whole
// system's event total for one closed hour against fixed upper/lower limits (0
// disables a side) and returns a site-level finding (CameraId 0) on breach. Unlike
// AnomalyScan it needs no history, so it works from day one.
func (s *Service) ManualScan(ctx context.Context, hourStart, upper, lower int64) ([]AnomalyFinding, error) {
	if s.rollups == nil || hourStart <= 0 {
		return nil, nil
	}
	hourStart -= hourStart % 3600

	filters := []sqldataenums.Filter{
		{FieldName: "BucketStart", Compare: sqldataenums.Equal, Value: hourStart},
	}
	var total int64
	var offset uint64
	for {
		page, pageTotal, err := s.rollups.Get(ctx, "", statsPageSize, offset, filters, nil)
		if err != nil {
			return nil, err
		}
		for _, row := range page {
			total += row.Count
		}
		offset += uint64(len(page))
		if len(page) == 0 || offset >= pageTotal || offset >= statsMaxRows {
			break
		}
	}

	var findings []AnomalyFinding
	if upper > 0 && total > upper {
		findings = append(findings, AnomalyFinding{Direction: AnomalyHigh, HourStart: hourStart, Actual: total, Hi: float64(upper)})
	}
	if lower > 0 && total < lower {
		findings = append(findings, AnomalyFinding{Direction: AnomalyLow, HourStart: hourStart, Actual: total, Lo: float64(lower)})
	}
	return findings, nil
}
