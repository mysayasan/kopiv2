package notification

import (
	"context"
	"strings"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// TallyRow is one (source, cameraId) group of notifications: how many there are and the worst
// severity among them. cameraId 0 means the group is source-level (not tied to a camera).
type TallyRow struct {
	Source   string `json:"source"`
	CameraId int64  `json:"cameraId"`
	Count    int    `json:"count"`
	Severity string `json:"severity"`
}

var tallySevRank = map[string]int{"critical": 3, "warning": 2, "info": 1}

func tallyWorse(a, b string) string {
	if tallySevRank[b] > tallySevRank[a] {
		return b
	}
	return a
}

// Tally aggregates notifications into compact (source, cameraId) groups on the SERVER, so a caller
// (e.g. the fleet map's per-node / per-camera badges) receives a small set of counts instead of
// paging thousands of raw rows to the browser and bucketing them there — which silently under-
// counted past whatever fixed limit the client asked for. It pages the store internally and puts
// no cap on how many notifications are summarised (only a large runaway backstop).
func (s *Service) Tally(ctx context.Context, unreadOnly bool) ([]TallyRow, error) {
	var filters []sqldataenums.Filter
	if unreadOnly {
		filters = append(filters, sqldataenums.Filter{FieldName: "IsRead", Compare: sqldataenums.Equal, Value: false})
	}
	type key struct {
		source string
		cam    int64
	}
	agg := map[key]*TallyRow{}
	// A stable sort is required so paging can't skip or double-count a row across page boundaries.
	sorters := []sqldataenums.Sorter{{FieldName: "Id", Sort: sqldataenums.ASC}}
	const batch = 1000
	var offset uint64
	for {
		items, _, err := s.repo.Get(ctx, "", batch, offset, filters, sorters)
		if err != nil {
			// Some backends signal "nothing matched" as an error rather than an empty page.
			if strings.Contains(strings.ToLower(err.Error()), "no result") {
				break
			}
			return nil, err
		}
		for _, n := range items {
			k := key{n.Source, n.CameraId}
			row := agg[k]
			if row == nil {
				row = &TallyRow{Source: n.Source, CameraId: n.CameraId, Severity: "info"}
				agg[k] = row
			}
			row.Count++
			sev := strings.ToLower(n.Severity)
			if sev == "" {
				sev = "info"
			}
			row.Severity = tallyWorse(row.Severity, sev)
		}
		if uint64(len(items)) < batch {
			break
		}
		offset += batch
		if offset >= 200000 { // runaway backstop
			break
		}
	}
	out := make([]TallyRow, 0, len(agg))
	for _, r := range agg {
		out = append(out, *r)
	}
	return out, nil
}
