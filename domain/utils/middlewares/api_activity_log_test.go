package middlewares

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/infra/telemetry"
)

type fakeApiActivityLogService struct {
	lastLog *entities.ApiLog
}

type fakeTelemetryRecorder struct {
	lastMetric telemetry.APIRequestMetric
}

func (f *fakeTelemetryRecorder) ObserveAPIRequest(metric telemetry.APIRequestMetric) {
	f.lastMetric = metric
}

func (f *fakeApiActivityLogService) Create(_ context.Context, model entities.ApiLog) (uint64, error) {
	copy := model
	f.lastLog = &copy
	return 1, nil
}

func TestApiActivityLogMiddlewareRecordsNonAuthRequest(t *testing.T) {
	serv := &fakeApiActivityLogService{}
	auth := NewAuth("test-secret")
	mid := NewApiActivityLog(serv, auth, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/health?x=1", nil)
	req.RemoteAddr = "192.0.2.10:12345"
	rr := httptest.NewRecorder()

	handler := mid.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	handler.ServeHTTP(rr, req)

	if serv.lastLog == nil {
		t.Fatal("expected ApiLog row to be created")
	}
	if serv.lastLog.CreatedBy != 0 {
		t.Fatalf("expected non-auth createdBy=0, got %d", serv.lastLog.CreatedBy)
	}
	if serv.lastLog.StatsCode != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, serv.lastLog.StatsCode)
	}
	if serv.lastLog.ClientIpAddrV4 != "192.0.2.10" {
		t.Fatalf("expected client IPv4, got %#v", serv.lastLog)
	}
	if serv.lastLog.RequestUrl != "/api/health?x=1" {
		t.Fatalf("expected request URL to include query, got %q", serv.lastLog.RequestUrl)
	}
	if serv.lastLog.DurationMs < 0 {
		t.Fatalf("expected non-negative duration, got %d", serv.lastLog.DurationMs)
	}
}

func TestApiActivityLogMiddlewareRecordsTelemetryWithRouteTemplate(t *testing.T) {
	serv := &fakeApiActivityLogService{}
	auth := NewAuth("test-secret")
	recorder := &fakeTelemetryRecorder{}
	mid := NewApiActivityLog(
		serv,
		auth,
		nil,
		WithApiActivityAppName("mymatasan"),
		WithApiActivityTelemetry(recorder),
	)

	router := mux.NewRouter()
	router.Handle("/api/items/{id}", mid.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))).Methods(http.MethodGet)

	req := httptest.NewRequest(http.MethodGet, "/api/items/123", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if recorder.lastMetric.AppName != "mymatasan" {
		t.Fatalf("expected app label, got %q", recorder.lastMetric.AppName)
	}
	if recorder.lastMetric.Method != http.MethodGet {
		t.Fatalf("expected method %s, got %s", http.MethodGet, recorder.lastMetric.Method)
	}
	if recorder.lastMetric.Path != "/api/items/{id}" {
		t.Fatalf("expected route template path, got %q", recorder.lastMetric.Path)
	}
	if recorder.lastMetric.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, recorder.lastMetric.StatusCode)
	}
	if recorder.lastMetric.DurationMs < 0 {
		t.Fatalf("expected non-negative duration, got %d", recorder.lastMetric.DurationMs)
	}
}

func TestApiActivityLogMiddlewareRecordsAuthenticatedActor(t *testing.T) {
	serv := &fakeApiActivityLogService{}
	auth := NewAuth("test-secret")
	mid := NewApiActivityLog(serv, auth, nil)
	token, err := auth.JwtToken(models.JwtCustomClaims{
		Id:    42,
		Email: "user@example.com",
		Name:  "User",
	})
	if err != nil {
		t.Fatalf("failed to build token: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/user-group", nil)
	req.AddCookie(&http.Cookie{Name: AuthCookieNameForRequest(req), Value: token})
	req.Header.Set("X-Forwarded-For", "2001:db8::1, 192.0.2.10")
	rr := httptest.NewRecorder()

	handler := mid.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	handler.ServeHTTP(rr, req)

	if serv.lastLog == nil {
		t.Fatal("expected ApiLog row to be created")
	}
	if serv.lastLog.CreatedBy != 42 {
		t.Fatalf("expected authenticated createdBy=42, got %d", serv.lastLog.CreatedBy)
	}
	if serv.lastLog.ClientIpAddrV6 != "2001:db8::1" {
		t.Fatalf("expected forwarded IPv6, got %#v", serv.lastLog)
	}
	if serv.lastLog.StatsCode != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, serv.lastLog.StatsCode)
	}
	if serv.lastLog.DurationMs < 0 {
		t.Fatalf("expected non-negative duration, got %d", serv.lastLog.DurationMs)
	}
}
