package notification

import (
	"context"
	"sort"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// NoisyCamera tallies a camera's AI-alert volume over the window and how many of
// those alerts are still unread (a proxy for "unreviewed / likely noise").
type NoisyCamera struct {
	CameraId int64 `json:"cameraId"`
	Count    int64 `json:"count"`
	Unread   int64 `json:"unread"`
}

// Noise is the alert-noise scorecard: the cameras generating the most AI alerts,
// with their unreviewed ratio, so an operator can spot tuning candidates (a camera
// firing constantly and being ignored is usually mis-tuned).
type Noise struct {
	From    int64         `json:"from"`
	To      int64         `json:"to"`
	Cameras []NoisyCamera `json:"cameras"`
}

// NoisyCameras returns the top `limit` cameras by AI-alert volume over [from, to]
// with their unread counts. Both totals and unread are counted from the same
// single pass over the notifications table so they share one denominator (unread is
// always ≤ total) — mixing a rollup total with a live unread count would let unread
// exceed total at the window/bucket boundary.
func (s *Service) NoisyCameras(ctx context.Context, from, to int64, limit int) (*Noise, error) {
	out := &Noise{From: from, To: to}
	if to <= 0 {
		to = 1<<62 - 1
	}
	if from < 0 {
		from = 0
	}
	if limit <= 0 {
		limit = 8
	}

	totals := map[int64]int64{}
	unread := map[int64]int64{}
	filters := []sqldataenums.Filter{
		{FieldName: "Category", Compare: sqldataenums.Equal, Value: string(CategoryVisionAlert)},
		{FieldName: "CreatedAt", Compare: sqldataenums.GreaterThanOrEqualTo, Value: from},
		{FieldName: "CreatedAt", Compare: sqldataenums.LessThanOrEqualTo, Value: to},
	}
	var offset uint64
	for {
		page, total, err := s.repo.Get(ctx, "", statsPageSize, offset, filters, nil)
		if err != nil {
			return nil, err
		}
		for _, n := range page {
			if n.CameraId <= 0 {
				continue
			}
			totals[n.CameraId]++
			if !n.IsRead {
				unread[n.CameraId]++
			}
		}
		offset += uint64(len(page))
		if len(page) == 0 || offset >= total || offset >= statsMaxRows {
			break
		}
	}

	out.Cameras = make([]NoisyCamera, 0, len(totals))
	for cameraId, count := range totals {
		out.Cameras = append(out.Cameras, NoisyCamera{CameraId: cameraId, Count: count, Unread: unread[cameraId]})
	}
	sort.Slice(out.Cameras, func(i, j int) bool {
		if out.Cameras[i].Count != out.Cameras[j].Count {
			return out.Cameras[i].Count > out.Cameras[j].Count
		}
		return out.Cameras[i].CameraId < out.Cameras[j].CameraId
	})
	if len(out.Cameras) > limit {
		out.Cameras = out.Cameras[:limit]
	}
	return out, nil
}
