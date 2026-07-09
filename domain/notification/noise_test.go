package notification

import (
	"context"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
)

func TestNoisyCamerasRanksByVolumeWithUnread(t *testing.T) {
	va := string(CategoryVisionAlert)
	// Total and unread are counted from the same notification scan, so unread ≤ total.
	// cam1: 5 alerts (3 unread), cam2: 3 (0 unread), cam3: 2 (2 unread).
	rows := []*entities.Notification{
		{Id: 1, CameraId: 1, Category: va, IsRead: false},
		{Id: 2, CameraId: 1, Category: va, IsRead: false},
		{Id: 3, CameraId: 1, Category: va, IsRead: false},
		{Id: 4, CameraId: 1, Category: va, IsRead: true},
		{Id: 5, CameraId: 1, Category: va, IsRead: true},
		{Id: 6, CameraId: 2, Category: va, IsRead: true},
		{Id: 7, CameraId: 2, Category: va, IsRead: true},
		{Id: 8, CameraId: 2, Category: va, IsRead: true},
		{Id: 9, CameraId: 3, Category: va, IsRead: false},
		{Id: 10, CameraId: 3, Category: va, IsRead: false},
	}
	s := &Service{repo: &fakeStatsRepo{rows: rows}}

	noise, err := s.NoisyCameras(context.Background(), 0, 1<<40, 8)
	if err != nil {
		t.Fatalf("NoisyCameras: %v", err)
	}
	if len(noise.Cameras) != 3 {
		t.Fatalf("cameras = %d, want 3", len(noise.Cameras))
	}
	if c := noise.Cameras[0]; c.CameraId != 1 || c.Count != 5 || c.Unread != 3 {
		t.Errorf("top = %+v, want cam1 count 5 unread 3", c)
	}
	if c := noise.Cameras[1]; c.CameraId != 2 || c.Count != 3 || c.Unread != 0 {
		t.Errorf("2nd = %+v, want cam2 count 3 unread 0", c)
	}
	if c := noise.Cameras[2]; c.CameraId != 3 || c.Count != 2 || c.Unread != 2 {
		t.Errorf("3rd = %+v, want cam3 count 2 unread 2", c)
	}
}
