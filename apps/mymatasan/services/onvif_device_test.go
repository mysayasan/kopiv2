package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/rtsp"
)

func TestRTSPProbeCandidatesAddsVIGIStreamPathForMainProfile(t *testing.T) {
	got := rtspProbeCandidates("rtsp://192.168.1.40:554/profile1/media.smp", "MainStream")
	want := []string{
		"rtsp://192.168.1.40:554/profile1/media.smp",
		"rtsp://192.168.1.40:554/stream1",
		"rtsp://192.168.1.40:554/stream2",
	}
	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRTSPProbeCandidatesKeepsExistingVIGIStreamPathUnique(t *testing.T) {
	got := rtspProbeCandidates("rtsp://192.168.1.40:554/stream2", "SubStream")
	want := []string{
		"rtsp://192.168.1.40:554/stream2",
		"rtsp://192.168.1.40:554/stream1",
	}
	if len(got) != len(want) {
		t.Fatalf("candidate count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveStreamPersistsWorkingFallbackCandidate(t *testing.T) {
	camRepo := &fakeCameraRepo{camera: entities.Camera{
		Id:   7,
		Host: "192.168.1.40",
	}}
	ovRepo := &fakeCameraOnvifRepo{ov: entities.CameraOnvif{
		Id:           1,
		CameraId:     7,
		XAddr:        "http://192.168.1.40/onvif/device_service",
		MediaXAddr:   "http://192.168.1.40/onvif/media_service",
		Username:     "camera-user",
		Password:     "camera-password",
		ProfileToken: "SubStream",
	}}
	client := &fakeResolveStreamClient{
		rtspURL:     "rtsp://192.168.1.40:554/profile1/media.smp",
		snapshotURI: "http://192.168.1.40/snapshot.jpg",
	}
	rtspClient := &fakeResolveRTSPClient{workingPath: "/stream2"}
	service := NewCameraService(camRepo, ovRepo, client, rtspClient)

	device, err := service.ResolveStream(context.Background(), 7, StreamSelectionRequest{
		Credentials:  onvif.Credentials{Username: "camera-user", Password: "camera-password"},
		ProfileToken: "SubStream",
	})
	if err != nil {
		t.Fatalf("ResolveStream() error = %v", err)
	}
	if device.RTSPUrl != "rtsp://192.168.1.40:554/stream2" {
		t.Fatalf("RTSPUrl = %q, want stream2 fallback", device.RTSPUrl)
	}
	if camRepo.camera.RTSPUrl != device.RTSPUrl {
		t.Fatalf("persisted RTSPUrl = %q, want %q", camRepo.camera.RTSPUrl, device.RTSPUrl)
	}
	if camRepo.camera.RTSPStatus != "online" {
		t.Fatalf("RTSPStatus = %q, want online", camRepo.camera.RTSPStatus)
	}
	if !strings.Contains(camRepo.camera.RTSPTracks, "H265") {
		t.Fatalf("RTSPTracks = %q, want marshaled probe tracks", camRepo.camera.RTSPTracks)
	}
	if len(rtspClient.probed) < 2 || !strings.Contains(rtspClient.probed[1], "/stream2") {
		t.Fatalf("probed candidates = %#v, want fallback stream2 after ONVIF URL", rtspClient.probed)
	}
}

func TestResolveStreamCanSwitchBackAndForth(t *testing.T) {
	camRepo := &fakeCameraRepo{camera: entities.Camera{
		Id:   9,
		Host: "192.168.1.40",
	}}
	ovRepo := &fakeCameraOnvifRepo{ov: entities.CameraOnvif{
		Id:           1,
		CameraId:     9,
		XAddr:        "http://192.168.1.40/onvif/device_service",
		MediaXAddr:   "http://192.168.1.40/onvif/media_service",
		Username:     "camera-user",
		Password:     "camera-password",
		ProfileToken: "MainStream",
	}}
	client := &fakeResolveStreamClient{
		streams: map[string]string{
			"MainStream": "rtsp://192.168.1.40:554/profile1/media.smp",
			"SubStream":  "rtsp://192.168.1.40:554/profile2/media.smp",
		},
		snapshotURI: "http://192.168.1.40/snapshot.jpg",
	}
	rtspClient := &fakeResolveRTSPClient{workingCodecs: map[string]string{
		"/stream1": "H265",
		"/stream2": "H264",
	}}
	service := NewCameraService(camRepo, ovRepo, client, rtspClient)

	main, err := service.ResolveStream(context.Background(), 9, StreamSelectionRequest{ProfileToken: "MainStream"})
	if err != nil {
		t.Fatalf("ResolveStream(main) error = %v", err)
	}
	if main.RTSPUrl != "rtsp://192.168.1.40:554/stream1" || !strings.Contains(main.RTSPTracks, "H265") {
		t.Fatalf("main stream state = url %q tracks %q", main.RTSPUrl, main.RTSPTracks)
	}

	sub, err := service.ResolveStream(context.Background(), 9, StreamSelectionRequest{ProfileToken: "SubStream"})
	if err != nil {
		t.Fatalf("ResolveStream(sub) error = %v", err)
	}
	if sub.RTSPUrl != "rtsp://192.168.1.40:554/stream2" || !strings.Contains(sub.RTSPTracks, "H264") {
		t.Fatalf("sub stream state = url %q tracks %q", sub.RTSPUrl, sub.RTSPTracks)
	}

	mainAgain, err := service.ResolveStream(context.Background(), 9, StreamSelectionRequest{ProfileToken: "MainStream"})
	if err != nil {
		t.Fatalf("ResolveStream(main again) error = %v", err)
	}
	if mainAgain.RTSPUrl != "rtsp://192.168.1.40:554/stream1" || !strings.Contains(mainAgain.RTSPTracks, "H265") {
		t.Fatalf("main again stream state = url %q tracks %q", mainAgain.RTSPUrl, mainAgain.RTSPTracks)
	}
}

// — Fake repos ---------------------------------------------------------------

type fakeCameraRepo struct {
	camera entities.Camera
}

func (f *fakeCameraRepo) Get(context.Context, string, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter) ([]*entities.Camera, uint64, error) {
	return nil, 0, nil
}
func (f *fakeCameraRepo) GetJoin(context.Context, string, any, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, ...string) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}
func (f *fakeCameraRepo) GetJoinWithSpec(context.Context, string, any, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, ...dbsql.JoinSpec) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}
func (f *fakeCameraRepo) GetSingle(_ context.Context, _ string, _ []sqldataenums.Filter) (*entities.Camera, error) {
	return nil, errors.New("no result found")
}
func (f *fakeCameraRepo) GetById(_ context.Context, _ string, _ uint64) (*entities.Camera, error) {
	cam := f.camera
	return &cam, nil
}
func (f *fakeCameraRepo) GetByUnique(_ context.Context, _ string, _ string, _ ...any) (*entities.Camera, error) {
	return nil, errors.New("no result found")
}
func (f *fakeCameraRepo) GetByForeign(_ context.Context, _ string, _ string, _ ...any) ([]*entities.Camera, error) {
	return nil, nil
}
func (f *fakeCameraRepo) Create(_ context.Context, _ string, m entities.Camera) (uint64, error) {
	f.camera = m
	return uint64(m.Id), nil
}
func (f *fakeCameraRepo) CreateMultiple(_ context.Context, _ string, _ []entities.Camera) (uint64, error) {
	return 0, nil
}
func (f *fakeCameraRepo) UpdateById(_ context.Context, _ string, m entities.Camera) (uint64, error) {
	f.camera = m
	return uint64(m.Id), nil
}
func (f *fakeCameraRepo) UpdateByUnique(_ context.Context, _ string, _ string, _ entities.Camera) (uint64, error) {
	return 0, nil
}
func (f *fakeCameraRepo) UpdateByForeign(_ context.Context, _ string, _ string, _ entities.Camera) (uint64, error) {
	return 0, nil
}
func (f *fakeCameraRepo) Delete(_ context.Context, _ string, _ []sqldataenums.Filter) (uint64, error) {
	return 0, nil
}
func (f *fakeCameraRepo) DeleteById(_ context.Context, _ string, _ uint64) (uint64, error) {
	return 0, nil
}
func (f *fakeCameraRepo) DeleteByUnique(_ context.Context, _ string, _ string, _ ...any) (uint64, error) {
	return 0, nil
}
func (f *fakeCameraRepo) DeleteByForeign(_ context.Context, _ string, _ string, _ ...any) (uint64, error) {
	return 0, nil
}

type fakeCameraOnvifRepo struct {
	ov entities.CameraOnvif
}

func (f *fakeCameraOnvifRepo) Get(context.Context, string, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter) ([]*entities.CameraOnvif, uint64, error) {
	return nil, 0, nil
}
func (f *fakeCameraOnvifRepo) GetJoin(context.Context, string, any, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, ...string) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}
func (f *fakeCameraOnvifRepo) GetJoinWithSpec(context.Context, string, any, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, ...dbsql.JoinSpec) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}
func (f *fakeCameraOnvifRepo) GetSingle(_ context.Context, _ string, _ []sqldataenums.Filter) (*entities.CameraOnvif, error) {
	return nil, errors.New("no result found")
}
func (f *fakeCameraOnvifRepo) GetById(_ context.Context, _ string, _ uint64) (*entities.CameraOnvif, error) {
	ov := f.ov
	return &ov, nil
}
func (f *fakeCameraOnvifRepo) GetByUnique(_ context.Context, _ string, _ string, _ ...any) (*entities.CameraOnvif, error) {
	ov := f.ov
	return &ov, nil
}
func (f *fakeCameraOnvifRepo) GetByForeign(_ context.Context, _ string, _ string, _ ...any) ([]*entities.CameraOnvif, error) {
	return nil, nil
}
func (f *fakeCameraOnvifRepo) Create(_ context.Context, _ string, m entities.CameraOnvif) (uint64, error) {
	f.ov = m
	return uint64(m.Id), nil
}
func (f *fakeCameraOnvifRepo) CreateMultiple(_ context.Context, _ string, _ []entities.CameraOnvif) (uint64, error) {
	return 0, nil
}
func (f *fakeCameraOnvifRepo) UpdateById(_ context.Context, _ string, m entities.CameraOnvif) (uint64, error) {
	f.ov = m
	return uint64(m.Id), nil
}
func (f *fakeCameraOnvifRepo) UpdateByUnique(_ context.Context, _ string, _ string, _ entities.CameraOnvif) (uint64, error) {
	return 0, nil
}
func (f *fakeCameraOnvifRepo) UpdateByForeign(_ context.Context, _ string, _ string, _ entities.CameraOnvif) (uint64, error) {
	return 0, nil
}
func (f *fakeCameraOnvifRepo) Delete(_ context.Context, _ string, _ []sqldataenums.Filter) (uint64, error) {
	return 0, nil
}
func (f *fakeCameraOnvifRepo) DeleteById(_ context.Context, _ string, _ uint64) (uint64, error) {
	return 0, nil
}
func (f *fakeCameraOnvifRepo) DeleteByUnique(_ context.Context, _ string, _ string, _ ...any) (uint64, error) {
	return 0, nil
}
func (f *fakeCameraOnvifRepo) DeleteByForeign(_ context.Context, _ string, _ string, _ ...any) (uint64, error) {
	return 0, nil
}

// — Fake ONVIF and RTSP clients ----------------------------------------------

type fakeResolveStreamClient struct {
	rtspURL     string
	streams     map[string]string
	snapshotURI string
}

func (f *fakeResolveStreamClient) Discover(context.Context, time.Duration) ([]onvif.Device, error) {
	return nil, nil
}
func (f *fakeResolveStreamClient) Probe(context.Context, string) (*onvif.Device, error) {
	return nil, nil
}
func (f *fakeResolveStreamClient) GetCapabilities(context.Context, string, onvif.Credentials) (*onvif.CapabilitiesResult, error) {
	return &onvif.CapabilitiesResult{MediaXAddr: "http://192.168.1.40/onvif/media_service"}, nil
}
func (f *fakeResolveStreamClient) GetStreamURI(_ context.Context, req onvif.StreamURIRequest) (*onvif.StreamURIResult, error) {
	rtspURL := f.rtspURL
	if f.streams != nil && f.streams[req.ProfileToken] != "" {
		rtspURL = f.streams[req.ProfileToken]
	}
	return &onvif.StreamURIResult{MediaXAddr: req.MediaServiceURL, ProfileToken: req.ProfileToken, RTSPURL: rtspURL}, nil
}
func (f *fakeResolveStreamClient) GetStreamOptions(context.Context, onvif.StreamURIRequest) (*onvif.StreamOptionsResult, error) {
	return nil, nil
}
func (f *fakeResolveStreamClient) GetSnapshotURI(_ context.Context, req onvif.StreamURIRequest) (*onvif.SnapshotURIResult, error) {
	return &onvif.SnapshotURIResult{MediaXAddr: req.MediaServiceURL, ProfileToken: req.ProfileToken, SnapshotURI: f.snapshotURI}, nil
}
func (f *fakeResolveStreamClient) ChangeUserPassword(context.Context, onvif.ChangeUserPasswordRequest) error {
	return nil
}
func (f *fakeResolveStreamClient) PTZMove(context.Context, onvif.PTZMoveRequest) error { return nil }
func (f *fakeResolveStreamClient) PTZStop(context.Context, onvif.PTZMoveRequest) error { return nil }

type fakeResolveRTSPClient struct {
	workingPath   string
	workingCodecs map[string]string
	probed        []string
}

func (f *fakeResolveRTSPClient) Probe(_ context.Context, uri string, _ rtsp.OpenOptions) (*rtsp.ProbeResult, error) {
	f.probed = append(f.probed, uri)
	codec := ""
	for path, value := range f.workingCodecs {
		if strings.Contains(uri, path) {
			codec = value
			break
		}
	}
	if codec == "" && f.workingPath != "" && strings.Contains(uri, f.workingPath) {
		codec = "H265"
	}
	if codec == "" {
		return nil, errors.New("describe RTSP stream failed: bad status code: 406 (Not Acceptable)")
	}
	return &rtsp.ProbeResult{
		URI:       uri,
		Transport: "tcp",
		Tracks: []rtsp.Track{{
			MediaType: "video",
			Codec:     codec,
			ClockRate: 90000,
		}},
	}, nil
}
