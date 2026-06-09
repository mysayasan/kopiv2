package apis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	"github.com/mysayasan/kopiv2/apps/mymatasan/services"
	"github.com/mysayasan/kopiv2/infra/onvif"
	"github.com/mysayasan/kopiv2/infra/rtsp"
)

type fakeOnvifDeviceService struct {
	discovered []onvif.Device
	saved      []onvif.Device
	entities   []entities.OnvifDevice
}

func (f *fakeOnvifDeviceService) Discover(context.Context, int64) ([]onvif.Device, error) {
	return f.discovered, nil
}

func (f *fakeOnvifDeviceService) Probe(context.Context, string) (*onvif.Device, error) {
	return nil, nil
}

func (f *fakeOnvifDeviceService) Get(context.Context, uint64, uint64) ([]*entities.OnvifDevice, uint64, error) {
	return nil, 0, nil
}

func (f *fakeOnvifDeviceService) Save(_ context.Context, device entities.OnvifDevice) (uint64, error) {
	f.entities = append(f.entities, device)
	return uint64(len(f.entities)), nil
}

func (f *fakeOnvifDeviceService) SaveDiscovered(_ context.Context, device onvif.Device) (uint64, error) {
	f.saved = append(f.saved, device)
	return uint64(len(f.saved)), nil
}

func (f *fakeOnvifDeviceService) SaveCredentials(context.Context, uint64, onvif.Credentials) (*entities.OnvifDevice, error) {
	return nil, nil
}

func (f *fakeOnvifDeviceService) ChangeCameraPassword(context.Context, uint64, services.ChangeCameraPasswordRequest) (*entities.OnvifDevice, error) {
	return nil, nil
}

func (f *fakeOnvifDeviceService) ResolveStream(context.Context, uint64, onvif.Credentials) (*entities.OnvifDevice, error) {
	return nil, nil
}

func (f *fakeOnvifDeviceService) ResolveLiveView(context.Context, uint64, onvif.Credentials) (*entities.OnvifDevice, error) {
	return nil, nil
}

func (f *fakeOnvifDeviceService) PTZMove(context.Context, uint64, services.PTZMoveRequest) (*entities.OnvifDevice, error) {
	return nil, nil
}

func (f *fakeOnvifDeviceService) PTZStop(context.Context, uint64) (*entities.OnvifDevice, error) {
	return nil, nil
}

func (f *fakeOnvifDeviceService) SnapshotSource(context.Context, uint64) (services.SnapshotSource, error) {
	return services.SnapshotSource{}, nil
}

func (f *fakeOnvifDeviceService) TestStream(context.Context, uint64) (*rtsp.ProbeResult, error) {
	return nil, nil
}

func (f *fakeOnvifDeviceService) Delete(context.Context, uint64) (uint64, error) {
	return 0, nil
}

func TestDiscoverDoesNotPersistDiscoveredDevices(t *testing.T) {
	service := &fakeOnvifDeviceService{
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
		t.Fatalf("SaveDiscovered count = %d", len(service.saved))
	}
	if len(service.entities) != 0 {
		t.Fatalf("Save count = %d", len(service.entities))
	}
}

func TestSaveDiscoveredPersistsOnlyPostedDevice(t *testing.T) {
	service := &fakeOnvifDeviceService{}
	api := &onvifApi{serv: service}
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

	req := httptest.NewRequest(http.MethodPost, "/api/onvif/devices/discovered", strings.NewReader(string(body)))
	rr := httptest.NewRecorder()
	api.saveDiscovered(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if len(service.entities) != 1 {
		t.Fatalf("Save count = %d", len(service.entities))
	}
	if service.entities[0].Name != "Gate" {
		t.Fatalf("saved name = %q", service.entities[0].Name)
	}
	if service.entities[0].RTSPUrl != "rtsp://192.168.1.40/live" {
		t.Fatalf("saved rtspUrl = %q", service.entities[0].RTSPUrl)
	}
}
