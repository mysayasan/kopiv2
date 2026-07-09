package notification

import (
	"context"
	"math"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
)

func TestCameraReliabilityPairsOutagesAndOngoing(t *testing.T) {
	const from = int64(1_000_000)
	to := from + 7*86400 // 604800s window
	he := func(id, cam, at int64, meta string) *entities.Notification {
		return &entities.Notification{
			Id: id, CameraId: cam, CreatedAt: at,
			Category: string(CategoryHealthCheck), Source: cameraHealthSource, Metadata: meta,
		}
	}
	// cam1: a 1h outage that recovered. cam2: went offline near the end and stayed down.
	rows := []*entities.Notification{
		he(1, 1, from+3600, `{"status":"offline"}`),
		he(2, 1, from+7200, `{"status":"online","offlineFor":3600}`),
		he(3, 2, to-3600, `{"status":"offline"}`),
	}
	s := &Service{repo: &fakeStatsRepo{rows: rows}}

	rep, err := s.CameraReliability(context.Background(), from, to)
	if err != nil {
		t.Fatalf("CameraReliability: %v", err)
	}
	if len(rep.Cameras) != 2 {
		t.Fatalf("cameras = %d, want 2 (cam3 with no events omitted)", len(rep.Cameras))
	}
	byCam := map[int64]CameraReliability{}
	for _, c := range rep.Cameras {
		byCam[c.CameraId] = c
	}
	c1 := byCam[1]
	if c1.OfflineSeconds != 3600 || c1.Incidents != 1 || c1.CurrentlyOffline {
		t.Errorf("cam1 = %+v, want 3600s/1 incident/recovered", c1)
	}
	if math.Abs(c1.UptimePercent-99.4047) > 0.01 {
		t.Errorf("cam1 uptime = %.4f, want ~99.40", c1.UptimePercent)
	}
	c2 := byCam[2]
	if c2.OfflineSeconds != 3600 || c2.Incidents != 1 || !c2.CurrentlyOffline {
		t.Errorf("cam2 = %+v, want 3600s/1 incident/currently offline", c2)
	}
}

func TestCameraReliabilityClipsPreWindowOutage(t *testing.T) {
	const from = int64(1_000_000)
	to := from + 7*86400
	// A recovery whose reported downtime started BEFORE the window — only the
	// in-window portion should count.
	rows := []*entities.Notification{
		{Id: 1, CameraId: 5, CreatedAt: from + 600, Category: string(CategoryHealthCheck), Source: cameraHealthSource,
			Metadata: `{"status":"online","offlineFor":3600}`}, // started 3000s before `from`
	}
	s := &Service{repo: &fakeStatsRepo{rows: rows}}
	rep, _ := s.CameraReliability(context.Background(), from, to)
	if len(rep.Cameras) != 1 {
		t.Fatalf("cameras = %d, want 1", len(rep.Cameras))
	}
	// Only from..from+600 = 600s of the outage falls inside the window.
	if got := rep.Cameras[0].OfflineSeconds; got != 600 {
		t.Errorf("offlineSeconds = %d, want 600 (clipped to window start)", got)
	}
}
