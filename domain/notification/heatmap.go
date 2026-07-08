package notification

import (
	"context"
	"time"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// HeatmapCell is one populated slot of the activity grid: a local day-of-week
// (0=Sunday..6=Saturday) and hour-of-day (0..23) with its event count. Only
// non-empty cells are returned; the frontend fills the full 7×24 grid.
type HeatmapCell struct {
	Day   int   `json:"day"`
	Hour  int   `json:"hour"`
	Count int64 `json:"count"`
}

// Heatmap is the activity-rhythm payload: events folded into local
// day-of-week × hour-of-day slots over [From, To]. Max is the busiest cell's
// count so the frontend can scale its color ramp without a second pass.
type Heatmap struct {
	From  int64         `json:"from"`
	To    int64         `json:"to"`
	Total int64         `json:"total"`
	Max   int64         `json:"max"`
	Cells []HeatmapCell `json:"cells"`
}

// Heatmap aggregates the hourly rollup into a local day-of-week × hour-of-day
// grid over [from, to] (unix seconds, matched against rollup bucket starts),
// optionally scoped to one camera. tzOffsetSec shifts each UTC bucket to the
// viewer's clock so the grid reflects their local rhythm. Returns an empty grid
// when the rollup read path is not wired (e.g. control-plane apps).
func (s *Service) Heatmap(ctx context.Context, from, to, cameraId, tzOffsetSec int64) (*Heatmap, error) {
	out := &Heatmap{From: from, To: to}
	if s.rollups == nil {
		return out, nil
	}
	if to <= 0 {
		to = 1<<62 - 1
	}
	if from < 0 {
		from = 0
	}

	filters := []sqldataenums.Filter{
		{FieldName: "BucketStart", Compare: sqldataenums.GreaterThanOrEqualTo, Value: from},
		{FieldName: "BucketStart", Compare: sqldataenums.LessThanOrEqualTo, Value: to},
	}
	if cameraId > 0 {
		filters = append(filters, sqldataenums.Filter{FieldName: "CameraId", Compare: sqldataenums.Equal, Value: cameraId})
	}

	grid := map[[2]int]int64{}
	var offset uint64
	for {
		page, total, err := s.rollups.Get(ctx, "", statsPageSize, offset, filters, nil)
		if err != nil {
			return nil, err
		}
		for _, row := range page {
			// Shift the UTC bucket start into the viewer's local clock, then read the
			// weekday/hour off it (interpreting the shifted seconds as UTC).
			local := time.Unix(row.BucketStart+tzOffsetSec, 0).UTC()
			key := [2]int{int(local.Weekday()), local.Hour()}
			grid[key] += row.Count
			out.Total += row.Count
		}
		offset += uint64(len(page))
		// The generic repo caps every query at 100 rows regardless of the requested
		// limit, so a short page does NOT mean "drained" — page by offset until the
		// reported total is covered (mirrors stats.go's fetchWindow). Terminating on
		// len(page) < statsPageSize here would read only the first 100 rollup rows.
		if len(page) == 0 || offset >= total || offset >= statsMaxRows {
			break
		}
	}

	out.Cells = make([]HeatmapCell, 0, len(grid))
	for key, count := range grid {
		out.Cells = append(out.Cells, HeatmapCell{Day: key[0], Hour: key[1], Count: count})
		if count > out.Max {
			out.Max = count
		}
	}
	return out, nil
}
