package notification

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// CountItem is a single labelled tally used by the dashboard breakdowns
// (by category, severity, source, detected object). Sorted highest-first.
type CountItem struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// CameraCount tallies events for one camera. The human-readable name is
// resolved by the frontend from its cameras list, so only the id is returned.
type CameraCount struct {
	CameraId int64 `json:"cameraId"`
	Count    int64 `json:"count"`
}

// TimeBucket is one point on the events-over-time series: the bucket start
// (unix seconds) plus a per-category split so the chart can stack the bands.
type TimeBucket struct {
	Start      int64            `json:"start"`
	Total      int64            `json:"total"`
	ByCategory map[string]int64 `json:"byCategory"`
}

// Stats is the full dashboard payload aggregated from the notifications table.
// Everything is derived in Go from a single time-windowed row fetch, so the
// result is identical across the sqlite, postgres and mariadb backends (no
// dialect-specific GROUP BY / date SQL).
type Stats struct {
	From       int64         `json:"from"`
	To         int64         `json:"to"`
	Bucket     string        `json:"bucket"`
	Total      int64         `json:"total"`
	PrevTotal  int64         `json:"prevTotal"`
	Unread     int64         `json:"unread"`
	Critical   int64         `json:"critical"`
	Warning    int64         `json:"warning"`
	Info       int64         `json:"info"`
	ByCategory []CountItem   `json:"byCategory"`
	BySeverity []CountItem   `json:"bySeverity"`
	BySource   []CountItem   `json:"bySource"`
	TopCameras []CameraCount `json:"topCameras"`
	TopLabels  []CountItem   `json:"topLabels"`
	Timeseries []TimeBucket  `json:"timeseries"`
}

// statsPageSize bounds each aggregation fetch page; the loop keeps paging until
// the window is drained or the hard cap is hit.
const (
	statsPageSize = 5000
	statsMaxRows  = 200000
)

// Stats aggregates notification activity over [from, to] into the dashboard
// payload. bucketSeconds is the timeseries granularity (e.g. 3600 for hourly,
// 86400 for daily); tzOffsetSec shifts bucket boundaries to the viewer's local
// day/hour. It also fetches the immediately-preceding equal-length window so the
// UI can show a delta (PrevTotal). All aggregation is performed in Go for
// cross-backend parity.
func (s *Service) Stats(ctx context.Context, from, to, bucketSeconds, tzOffsetSec int64) (*Stats, error) {
	if to <= 0 {
		to = 1<<62 - 1
	}
	if from < 0 {
		from = 0
	}
	if bucketSeconds <= 0 {
		bucketSeconds = 86400
	}
	window := to - from
	prevFrom := from - window
	if prevFrom < 0 {
		prevFrom = 0
	}

	rows, err := s.fetchWindow(ctx, prevFrom, to)
	if err != nil {
		return nil, err
	}

	out := &Stats{From: from, To: to}
	switch bucketSeconds {
	case 3600:
		out.Bucket = "hour"
	case 86400:
		out.Bucket = "day"
	default:
		out.Bucket = "custom"
	}

	byCategory := map[string]int64{}
	bySeverity := map[string]int64{}
	bySource := map[string]int64{}
	byCamera := map[int64]int64{}
	byLabel := map[string]int64{}

	// Pre-seed the timeseries with empty buckets spanning [from, to] so gaps in
	// activity render as zeroes (a continuous axis) rather than collapsing.
	var series []*TimeBucket
	bucketIndex := map[int64]*TimeBucket{}
	firstBucket := bucketStart(from, bucketSeconds, tzOffsetSec)
	for start := firstBucket; start < to; start += bucketSeconds {
		tb := &TimeBucket{Start: start, ByCategory: map[string]int64{}}
		bucketIndex[start] = tb
		series = append(series, tb)
	}

	for _, n := range rows {
		if n.CreatedAt < from {
			// Belongs to the previous window: only counts toward the delta.
			out.PrevTotal++
			continue
		}
		if n.CreatedAt > to {
			continue
		}
		out.Total++
		cat := strings.TrimSpace(n.Category)
		if cat == "" {
			cat = "other"
		}
		byCategory[cat]++
		sev := strings.TrimSpace(n.Severity)
		if sev == "" {
			sev = "info"
		}
		bySeverity[sev]++
		switch sev {
		case "critical":
			out.Critical++
		case "warning":
			out.Warning++
		default:
			out.Info++
		}
		if src := strings.TrimSpace(n.Source); src != "" {
			bySource[src]++
		}
		if !n.IsRead {
			out.Unread++
		}
		if n.CameraId > 0 {
			byCamera[n.CameraId]++
		}
		if label := labelFromNotification(n); label != "" {
			byLabel[label]++
		}
		if tb := bucketIndex[bucketStart(n.CreatedAt, bucketSeconds, tzOffsetSec)]; tb != nil {
			tb.Total++
			tb.ByCategory[cat]++
		}
	}

	out.Timeseries = make([]TimeBucket, len(series))
	for i, tb := range series {
		out.Timeseries[i] = *tb
	}
	out.ByCategory = sortedCounts(byCategory, 0)
	out.BySeverity = sortedCounts(bySeverity, 0)
	out.BySource = sortedCounts(bySource, 0)
	out.TopLabels = sortedCounts(byLabel, 8)
	out.TopCameras = sortedCameraCounts(byCamera, 8)
	return out, nil
}

// fetchWindow pages the notifications created within [lo, hi] (inclusive),
// honoring the hard row cap so a runaway window cannot exhaust memory.
func (s *Service) fetchWindow(ctx context.Context, lo, hi int64) ([]*entities.Notification, error) {
	filters := []sqldataenums.Filter{
		{FieldName: "CreatedAt", Compare: sqldataenums.GreaterThanOrEqualTo, Value: lo},
		{FieldName: "CreatedAt", Compare: sqldataenums.LessThanOrEqualTo, Value: hi},
	}
	sorters := []sqldataenums.Sorter{{FieldName: "CreatedAt", Sort: sqldataenums.ASC}}

	var all []*entities.Notification
	var offset uint64
	for {
		page, total, err := s.repo.Get(ctx, "", statsPageSize, offset, filters, sorters)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		offset += uint64(len(page))
		// The generic repo caps every query at 100 rows regardless of the requested
		// limit, so a short page is NOT the drained signal — page by offset until the
		// reported total is covered. (A `len(page) < statsPageSize` check here read only
		// the first 100 rows, undercounting the dashboard and, for multi-window ranges,
		// dropping the whole current window into the previous one — total 0.)
		if len(page) == 0 || offset >= total || len(all) >= statsMaxRows {
			break
		}
	}
	return all, nil
}

// bucketStart returns the unix-second start of the bucket that a timestamp
// falls into, aligned to the viewer's local timezone so day/hour boundaries
// match what the operator sees on the clock.
func bucketStart(ts, bucketSeconds, tzOffsetSec int64) int64 {
	local := ts + tzOffsetSec
	aligned := local - ((local%bucketSeconds)+bucketSeconds)%bucketSeconds
	return aligned - tzOffsetSec
}

// labelFromNotification extracts a detected-object label for the "top objects"
// breakdown from a vision-alert notification's stored Data (Metadata JSON),
// preferring the object label, then the raw label, then the detection type.
// Returns "" for non-vision or unlabelled rows.
func labelFromNotification(n *entities.Notification) string {
	if n.Category != string(CategoryVisionAlert) || strings.TrimSpace(n.Metadata) == "" {
		return ""
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(n.Metadata), &data); err != nil {
		return ""
	}
	for _, key := range []string{"objectLabel", "label", "detectionType"} {
		if v, ok := data[key].(string); ok {
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				return strings.ReplaceAll(trimmed, "_", " ")
			}
		}
	}
	return ""
}

// sortedCounts turns a tally map into a highest-first slice, keeping at most
// limit entries (0 = keep all). Ties break alphabetically for stable output.
func sortedCounts(m map[string]int64, limit int) []CountItem {
	items := make([]CountItem, 0, len(m))
	for k, v := range m {
		items = append(items, CountItem{Key: k, Count: v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Key < items[j].Key
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

// sortedCameraCounts is sortedCounts for the camera tallies, keyed by id.
func sortedCameraCounts(m map[int64]int64, limit int) []CameraCount {
	items := make([]CameraCount, 0, len(m))
	for k, v := range m {
		items = append(items, CameraCount{CameraId: k, Count: v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].CameraId < items[j].CameraId
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}
