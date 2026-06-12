package apis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/rtsp"
)

type fakeCameraService struct {
	discovered []onvif.Device
	saved      []services.CameraDetail
}

func (f *fakeCameraService) Discover(_ context.Context, _ int64) ([]onvif.Device, error) {
	return f.discovered, nil
}
func (f *fakeCameraService) Probe(_ context.Context, _ string) (*onvif.Device, error) { return nil, nil }
func (f *fakeCameraService) Get(_ context.Context, _ uint64, _ uint64) ([]*services.CameraDetail, uint64, error) {
	return nil, 0, nil
}
func (f *fakeCameraService) GetById(_ context.Context, _ uint64) (*services.CameraDetail, error) {
	return nil, nil
}
func (f *fakeCameraService) Save(_ context.Context, detail services.CameraDetail) (uint64, error) {
	f.saved = append(f.saved, detail)
	return uint64(len(f.saved)), nil
}
func (f *fakeCameraService) SaveCredentials(_ context.Context, _ uint64, _ onvif.Credentials) (*services.CameraDetail, error) {
	return nil, nil
}
func (f *fakeCameraService) ChangeCameraPassword(_ context.Context, _ uint64, _ services.ChangeCameraPasswordRequest) (*services.CameraDetail, error) {
	return nil, nil
}
func (f *fakeCameraService) StreamOptions(_ context.Context, _ uint64, _ onvif.Credentials) (*onvif.StreamOptionsResult, error) {
	return nil, nil
}
func (f *fakeCameraService) ResolveStream(_ context.Context, _ uint64, _ services.StreamSelectionRequest) (*services.CameraDetail, error) {
	return nil, nil
}
func (f *fakeCameraService) SetLiveStream(_ context.Context, _ uint64, _ string) (*services.CameraDetail, error) {
	return nil, nil
}
func (f *fakeCameraService) ResolveLiveView(_ context.Context, _ uint64, _ onvif.Credentials) (*services.CameraDetail, error) {
	return nil, nil
}
func (f *fakeCameraService) PTZMove(_ context.Context, _ uint64, _ services.PTZMoveRequest) (*services.CameraDetail, error) {
	return nil, nil
}
func (f *fakeCameraService) PTZStop(_ context.Context, _ uint64) (*services.CameraDetail, error) {
	return nil, nil
}
func (f *fakeCameraService) SnapshotSource(_ context.Context, _ uint64) (services.SnapshotSource, error) {
	return services.SnapshotSource{}, nil
}
func (f *fakeCameraService) TestStream(_ context.Context, _ uint64) (*rtsp.ProbeResult, error) {
	return nil, nil
}
func (f *fakeCameraService) Delete(_ context.Context, _ uint64) (uint64, error) { return 0, nil }

func TestDiscoverDoesNotPersistDiscoveredDevices(t *testing.T) {
	service := &fakeCameraService{
		discovered: []onvif.Device{{
			Name:    "Gate",
			Host:    "192.168.1.40",
			Port:    80,
			XAddr:   "http://192.168.1.40/onvif/device_service",
			RTSPURL: "rtsp://192.168.1.40/live",
		}},
	}
	api := &onvifApi{serv: service}

	req := httptest.NewRequest(http.MethodPost, "/api/onvif/discover", strings.NewReader(`{"timeoutMs":100}`))
	rr := httptest.NewRecorder()
	api.discover(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if len(service.saved) != 0 {
		t.Fatalf("Save count = %d, want 0", len(service.saved))
	}
}

func TestSaveDiscoveredPersistsOnlyPostedDevice(t *testing.T) {
	service := &fakeCameraService{}
	api := &cameraApi{serv: service}
	body, err := json.Marshal(saveDiscoveredRequest{
		Device: onvif.Device{
			Name:    "Gate",
			Host:    "192.168.1.40",
			Port:    80,
			XAddr:   "http://192.168.1.40/onvif/device_service",
			RTSPURL: "rtsp://192.168.1.40/live",
		},
		Description: "front gate",
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/cameras/discovered", strings.NewReader(string(body)))
	rr := httptest.NewRecorder()
	api.saveDiscovered(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if len(service.saved) != 1 {
		t.Fatalf("Save count = %d, want 1", len(service.saved))
	}
	if service.saved[0].Name != "Gate" {
		t.Fatalf("saved name = %q", service.saved[0].Name)
	}
	if service.saved[0].RTSPUrl != "rtsp://192.168.1.40/live" {
		t.Fatalf("saved rtspUrl = %q", service.saved[0].RTSPUrl)
	}
}
