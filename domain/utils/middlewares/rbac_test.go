package middlewares

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mysayasan/kopiv2/domain/entities"
	enumauth "github.com/mysayasan/kopiv2/domain/enums/auth"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/infra/cache"
)

type fakeApiEndpointRbacService struct {
	calls map[uint64]int
}

type wildcardApiEndpointRbacService struct{}

func (m *fakeApiEndpointRbacService) Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiEndpointRbac, uint64, error) {
	return nil, 0, nil
}

func (m *fakeApiEndpointRbacService) GetApiEpByUserRole(ctx context.Context, userId uint64) ([]*entities.ApiEndpointRbacJoinModel, uint64, error) {
	if m.calls == nil {
		m.calls = map[uint64]int{}
	}
	m.calls[userId] += 1

	return []*entities.ApiEndpointRbacJoinModel{
		{
			Host:      "example.com",
			Path:      "/api/admin",
			CanGet:    true,
			CanPost:   true,
			CanPut:    true,
			CanDelete: true,
		},
	}, 1, nil
}

func (m *fakeApiEndpointRbacService) Create(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error) {
	return 0, nil
}

func (m *fakeApiEndpointRbacService) Update(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error) {
	return 0, nil
}

func (m *fakeApiEndpointRbacService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return 0, nil
}

func (m *fakeApiEndpointRbacService) Validate(ctx context.Context, host string, path string, userRoleId uint64) (*entities.ApiEndpointRbac, error) {
	return nil, nil
}

func (m *wildcardApiEndpointRbacService) Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiEndpointRbac, uint64, error) {
	return nil, 0, nil
}

func (m *wildcardApiEndpointRbacService) GetApiEpByUserRole(ctx context.Context, userId uint64) ([]*entities.ApiEndpointRbacJoinModel, uint64, error) {
	return []*entities.ApiEndpointRbacJoinModel{{Host: "*", Path: "/api/admin", CanGet: true}}, 1, nil
}

func (m *wildcardApiEndpointRbacService) Create(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error) {
	return 0, nil
}

func (m *wildcardApiEndpointRbacService) Update(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error) {
	return 0, nil
}

func (m *wildcardApiEndpointRbacService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return 0, nil
}

func (m *wildcardApiEndpointRbacService) Validate(ctx context.Context, host string, path string, userRoleId uint64) (*entities.ApiEndpointRbac, error) {
	return nil, nil
}

func TestRbacRejectsMissingClaims(t *testing.T) {
	service := &fakeApiEndpointRbacService{}
	m := NewRbac(service, cache.NewMemoryStore(time.Minute, time.Minute), time.Minute)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/admin/test", nil)
	rr := httptest.NewRecorder()

	handler := m.RbacHandler(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestRbacCachesByRoleId(t *testing.T) {
	service := &fakeApiEndpointRbacService{}
	m := NewRbac(service, cache.NewMemoryStore(time.Minute, time.Minute), time.Minute)

	handler := m.RbacHandler(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	call := func(roleID int64, userID int64) int {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/api/admin/test", nil)
		req.Host = "example.com"
		claims := &models.JwtCustomClaims{Id: userID, RoleId: roleID, Email: "user@example.com"}
		req = req.WithContext(context.WithValue(req.Context(), enumauth.Claims, claims))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
		return service.calls[uint64(userID)]
	}

	if got := call(1, 10); got != 1 {
		t.Fatalf("expected one RBAC lookup for role 1, got %d", got)
	}
	if got := call(1, 10); got != 1 {
		t.Fatalf("expected role 1 to use cache, got lookup count %d", got)
	}
	if got := call(2, 20); got != 1 {
		t.Fatalf("expected one RBAC lookup for role 2, got %d", got)
	}
}

func TestRbacAllowsWildcardHost(t *testing.T) {
	service := &wildcardApiEndpointRbacService{}
	m := NewRbac(service, cache.NewMemoryStore(time.Minute, time.Minute), time.Minute)

	handler := m.RbacHandler(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/admin/test", nil)
	req.Host = "example.com"
	req = req.WithContext(context.WithValue(req.Context(), enumauth.Claims, &models.JwtCustomClaims{Id: 1, RoleId: 1}))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestRbacRejectsPartialPathPrefix(t *testing.T) {
	service := &wildcardApiEndpointRbacService{}
	m := NewRbac(service, cache.NewMemoryStore(time.Minute, time.Minute), time.Minute)

	handler := m.RbacHandler(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/adminx", nil)
	req.Host = "example.com"
	req = req.WithContext(context.WithValue(req.Context(), enumauth.Claims, &models.JwtCustomClaims{Id: 1, RoleId: 1}))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}
