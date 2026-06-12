package middlewares

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	apiaccessenums "github.com/mysayasan/kopiv2/domain/enums/apiaccess"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/cache"
)

type fakeEndpointTierService struct {
	endpoints []*entities.ApiEndpoint
	calls     int
}

func (f *fakeEndpointTierService) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.ApiEndpoint, uint64, error) {
	f.calls++
	return f.endpoints, uint64(len(f.endpoints)), nil
}

func TestRateLimitSmokeCoversAllAccessTiers(t *testing.T) {
	service := &fakeEndpointTierService{
		endpoints: []*entities.ApiEndpoint{
			{Host: "*", Path: "/api/admin", AccessTier: apiaccessenums.DevOnly, IsActive: true},
			{Host: "*", Path: "/api/home", AccessTier: apiaccessenums.AuthOnly, IsActive: true},
			{Host: "*", Path: "/api/version", AccessTier: apiaccessenums.Public, IsActive: true},
		},
	}

	mid := NewRateLimit(service, cache.NewMemoryStore(time.Minute, time.Minute), nil, RateLimitConfig{
		Enabled:          true,
		EndpointCacheTTL: time.Minute,
		DevOnly:          RateLimitTierConfig{Enabled: true, Requests: 1, Window: time.Minute},
		AuthOnly:         RateLimitTierConfig{Enabled: true, Requests: 1, Window: time.Minute},
		Public:           RateLimitTierConfig{Enabled: true, Requests: 1, Window: time.Minute},
	})

	handler := mid.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "devOnly", path: "/api/admin/test"},
		{name: "authOnly", path: "/api/home/latest"},
		{name: "public", path: "/api/version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://example.com"+tc.path, nil)
			req.RemoteAddr = "192.0.2.10:1000"
			handler.ServeHTTP(first, req)
			if first.Code != http.StatusOK {
				t.Fatalf("first request status got %d want %d body=%s", first.Code, http.StatusOK, first.Body.String())
			}

			second := httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodGet, "http://example.com"+tc.path, nil)
			req.RemoteAddr = "192.0.2.10:1000"
			handler.ServeHTTP(second, req)
			if second.Code != http.StatusTooManyRequests {
				t.Fatalf("second request status got %d want %d body=%s", second.Code, http.StatusTooManyRequests, second.Body.String())
			}
			if second.Header().Get("Retry-After") == "" {
				t.Fatalf("expected Retry-After header")
			}
		})
	}
}

func TestRateLimitUsesLongestEndpointTierMatch(t *testing.T) {
	service := &fakeEndpointTierService{
		endpoints: []*entities.ApiEndpoint{
			{Host: "*", Path: "/api/file-storage", AccessTier: apiaccessenums.DevOnly, IsActive: true},
			{Host: "*", Path: "/api/file-storage/download", AccessTier: apiaccessenums.Public, IsActive: true},
		},
	}

	store := cache.NewMemoryStore(time.Minute, time.Minute)
	mid := NewRateLimit(service, store, nil, RateLimitConfig{
		Enabled:          true,
		EndpointCacheTTL: time.Minute,
		DevOnly:          RateLimitTierConfig{Enabled: true, Requests: 1, Window: time.Minute},
		Public:           RateLimitTierConfig{Enabled: true, Requests: 2, Window: time.Minute},
	})

	handler := mid.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.com/api/file-storage/download?id=1", nil)
		req.RemoteAddr = "192.0.2.20:1000"
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("download request %d status got %d want %d body=%s", i+1, rr.Code, http.StatusOK, rr.Body.String())
		}
	}
}
