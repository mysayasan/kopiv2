package notification

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// cameraHealthSource is the notification Source the camera-health monitor stamps on
// its offline/recovery events; reliability is derived from that event stream.
const cameraHealthSource = "camera-health-monitor"

// CameraReliability is one camera's reliability summary over the window: how much
// of it the camera was reachable, how long it was offline, how many separate
// outages, whether it is offline right now, and when it was last heard from.
type CameraReliability struct {
	CameraId         int64   `json:"cameraId"`
	UptimePercent    float64 `json:"uptimePercent"`
	OfflineSeconds   int64   `json:"offlineSeconds"`
	Incidents        int     `json:"incidents"`
	CurrentlyOffline bool    `json:"currentlyOffline"`
	LastEventAt      int64   `json:"lastEventAt"`
}

// Reliability is the camera-reliability scorecard payload. Cameras is worst-first
// and includes only cameras that had at least one health event in the window; the
// frontend merges it with the full camera list (absent cameras = 100% healthy).
type Reliability struct {
	From          int64               `json:"from"`
	To            int64               `json:"to"`
	WindowSeconds int64               `json:"windowSeconds"`
	Cameras       []CameraReliability `json:"cameras"`
}

// CameraReliability computes per-camera uptime/outage stats over [from, to] from
// the camera-health monitor's offline/recovery notifications. Downtime is derived
// by pairing offline→recovery events (clipped to the window), plus any still-open
// outage extended to `to`, so a camera offline at the window's end is counted.
func (s *Service) CameraReliability(ctx context.Context, from, to int64) (*Reliability, error) {
	if to <= 0 {
		to = 1<<62 - 1
	}
	if from < 0 {
		from = 0
	}
	out := &Reliability{From: from, To: to, WindowSeconds: to - from}

	filters := []sqldataenums.Filter{
		{FieldName: "Category", Compare: sqldataenums.Equal, Value: string(CategoryHealthCheck)},
		{FieldName: "Source", Compare: sqldataenums.Equal, Value: cameraHealthSource},
		{FieldName: "CreatedAt", Compare: sqldataenums.GreaterThanOrEqualTo, Value: from},
		{FieldName: "CreatedAt", Compare: sqldataenums.LessThanOrEqualTo, Value: to},
	}
	sorters := []sqldataenums.Sorter{{FieldName: "CreatedAt", Sort: sqldataenums.ASC}}

	byCamera := map[int64][]*entities.Notification{}
	var offset uint64
	for {
		page, total, err := s.repo.Get(ctx, "", statsPageSize, offset, filters, sorters)
		if err != nil {
			return nil, err
		}
		for _, n := range page {
			if n.CameraId > 0 {
				byCamera[n.CameraId] = append(byCamera[n.CameraId], n)
			}
		}
		offset += uint64(len(page))
		if len(page) == 0 || offset >= total || offset >= statsMaxRows {
			break
		}
	}

	window := to - from
	out.Cameras = make([]CameraReliability, 0, len(byCamera))
	for cameraId, events := range byCamera {
		out.Cameras = append(out.Cameras, reliabilityForCamera(cameraId, events, from, to, window))
	}
	// Worst uptime first, then most incidents, so the operator sees problems on top.
	sort.Slice(out.Cameras, func(i, j int) bool {
		if out.Cameras[i].UptimePercent != out.Cameras[j].UptimePercent {
			return out.Cameras[i].UptimePercent < out.Cameras[j].UptimePercent
		}
		return out.Cameras[i].Incidents > out.Cameras[j].Incidents
	})
	return out, nil
}

// reliabilityForCamera reduces one camera's chronologically-sorted health events
// into its reliability summary.
func reliabilityForCamera(cameraId int64, events []*entities.Notification, from, to, window int64) CameraReliability {
	var downtime int64
	incidents := 0
	open := false
	var offlineAt int64
	var lastAt int64

	for _, e := range events {
		lastAt = e.CreatedAt
		status, offlineFor := parseHealthEvent(e)
		switch status {
		case "offline":
			incidents++
			offlineAt = e.CreatedAt
			open = true
		case "online":
			// Recovery: prefer the reported downtime; fall back to the paired
			// offline event when this window opened the outage.
			dur := offlineFor
			if dur <= 0 && open {
				dur = e.CreatedAt - offlineAt
			}
			start := e.CreatedAt - dur
			if start < from {
				start = from
			}
			if e.CreatedAt > start {
				downtime += e.CreatedAt - start
			}
			open = false
		}
	}
	if open {
		start := offlineAt
		if start < from {
			start = from
		}
		if to > start {
			downtime += to - start
		}
	}

	uptime := 100.0
	if window > 0 {
		uptime = 100 * (1 - float64(downtime)/float64(window))
		if uptime < 0 {
			uptime = 0
		}
		if uptime > 100 {
			uptime = 100
		}
	}
	return CameraReliability{
		CameraId:         cameraId,
		UptimePercent:    uptime,
		OfflineSeconds:   downtime,
		Incidents:        incidents,
		CurrentlyOffline: open,
		LastEventAt:      lastAt,
	}
}

// parseHealthEvent pulls the reachability status and reported downtime from a
// camera-health notification's metadata.
func parseHealthEvent(n *entities.Notification) (status string, offlineFor int64) {
	if n.Metadata == "" {
		return "", 0
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(n.Metadata), &d); err != nil {
		return "", 0
	}
	if s, ok := d["status"].(string); ok {
		status = s
	}
	if v, ok := d["offlineFor"].(float64); ok {
		offlineFor = int64(v)
	}
	return status, offlineFor
}
