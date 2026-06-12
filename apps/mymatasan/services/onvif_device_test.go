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
	repo := &fakeOnvifRepo{device: entities.OnvifDevice{
		Id:           7,
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
	service := NewOnvifDeviceService(repo, client, rtspClient)

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
	if repo.device.RTSPUrl != device.RTSPUrl {
		t.Fatalf("persisted RTSPUrl = %q, want %q", repo.device.RTSPUrl, device.RTSPUrl)
	}
	if repo.device.RTSPStatus != "online" {
		t.Fatalf("RTSPStatus = %q, want online", repo.device.RTSPStatus)
	}
	if !strings.Contains(repo.device.RTSPTracks, "H265") {
		t.Fatalf("RTSPTracks = %q, want marshaled probe tracks", repo.device.RTSPTracks)
	}
	if len(rtspClient.probed) < 2 || !strings.Contains(rtspClient.probed[1], "/stream2") {
		t.Fatalf("probed candidates = %#v, want fallback stream2 after ONVIF URL", rtspClient.probed)
	}
}

func TestResolveStreamCanSwitchBackAndForth(t *testing.T) {
	repo := &fakeOnvifRepo{device: entities.OnvifDevice{
		Id:           9,
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
	service := NewOnvifDeviceService(repo, client, rtspClient)

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

type fakeOnvifRepo struct {
	device entities.OnvifDevice
}

func (f *fakeOnvifRepo) Get(context.Context, string, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter) ([]*entities.OnvifDevice, uint64, error) {
	return nil, 0, nil
}

func (f *fakeOnvifRepo) GetJoin(context.Context, string, any, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, ...string) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}

func (f *fakeOnvifRepo) GetJoinWithSpec(context.Context, string, any, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter, ...dbsql.JoinSpec) ([]map[string]any, uint64, error) {
	return nil, 0, nil
}

func (f *fakeOnvifRepo) GetSingle(context.Context, string, []sqldataenums.Filter) (*entities.OnvifDevice, error) {
	return nil, nil
}

func (f *fakeOnvifRepo) GetById(context.Context, string, uint64) (*entities.OnvifDevice, error) {
	device := f.device
	return &device, nil
}

func (f *fakeOnvifRepo) GetByUnique(context.Context, string, string, ...any) (*entities.OnvifDevice, error) {
	return nil, nil
}

func (f *fakeOnvifRepo) GetByForeign(context.Context, string, string, ...any) ([]*entities.OnvifDevice, error) {
	return nil, nil
}

func (f *fakeOnvifRepo) Create(context.Context, string, entities.OnvifDevice) (uint64, error) {
	return 0, nil
}

func (f *fakeOnvifRepo) CreateMultiple(context.Context, string, []entities.OnvifDevice) (uint64, error) {
	return 0, nil
}

func (f *fakeOnvifRepo) UpdateById(_ context.Context, _ string, model entities.OnvifDevice) (uint64, error) {
	f.device = model
	return uint64(model.Id), nil
}

func (f *fakeOnvifRepo) UpdateByUnique(context.Context, string, string, entities.OnvifDevice) (uint64, error) {
	return 0, nil
}

func (f *fakeOnvifRepo) UpdateByForeign(context.Context, string, string, entities.OnvifDevice) (uint64, error) {
	return 0, nil
}

func (f *fakeOnvifRepo) Delete(context.Context, string, []sqldataenums.Filter) (uint64, error) {
	return 0, nil
}

func (f *fakeOnvifRepo) DeleteById(context.Context, string, uint64) (uint64, error) {
	return 0, nil
}

func (f *fakeOnvifRepo) DeleteByUnique(context.Context, string, string, ...any) (uint64, error) {
	return 0, nil
}

func (f *fakeOnvifRepo) DeleteByForeign(context.Context, string, string, ...any) (uint64, error) {
	return 0, nil
}

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

func (f *fakeResolveStreamClient) PTZMove(context.Context, onvif.PTZMoveRequest) error {
	return nil
}

func (f *fakeResolveStreamClient) PTZStop(context.Context, onvif.PTZMoveRequest) error {
	return nil
}

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
