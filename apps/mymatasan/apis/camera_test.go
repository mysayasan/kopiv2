package apis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
)

type fakeCameraStreamService struct{}

func (m *fakeCameraStreamService) Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.CameraStream, uint64, error) {
	return nil, 0, nil
}

func (m *fakeCameraStreamService) GetById(ctx context.Context, groupId uint64) (*entities.CameraStream, error) {
	return nil, nil
}

func (m *fakeCameraStreamService) Create(ctx context.Context, model entities.CameraStream) (uint64, error) {
	return 0, nil
}

func (m *fakeCameraStreamService) Update(ctx context.Context, model entities.CameraStream) (uint64, error) {
	return 0, nil
}

func (m *fakeCameraStreamService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return 0, nil
}

func (m *fakeCameraStreamService) StartAllMjpegStream() error {
	return nil
}

func (m *fakeCameraStreamService) ReadMjpeg(ctx context.Context, id int64) <-chan []byte {
	ch := make(chan []byte)
	close(ch)
	return ch
}

func (m *fakeCameraStreamService) Shutdown(ctx context.Context) error {
	return nil
}

func TestGetMjpegStreamReturnsWhenFrameChannelCloses(t *testing.T) {
	api := &cameraApi{serv: &fakeCameraStreamService{}}
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/camera/stream/mjpeg/1", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		api.getMjpegStream(rr, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected MJPEG handler to return when frame channel closes")
	}
}
