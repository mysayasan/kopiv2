package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/mysayasan/kopiv2/apps/mymatasan/apis"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/domain/models"
	"github.com/mysayasan/kopiv2/domain/utils/middlewares"
	"github.com/mysayasan/kopiv2/infra/cache"
)

type fakeDbCrud struct {
	pingErr error
}

func (m *fakeDbCrud) Select(ctx context.Context, model interface{}, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter, datasrc string, joinsrc ...string) ([]map[string]interface{}, uint64, error) {
	return nil, 0, nil
}

func (m *fakeDbCrud) SelectSingle(ctx context.Context, model interface{}, filters []sqldataenums.Filter, datasrc string) (map[string]interface{}, error) {
	return nil, nil
}

func (m *fakeDbCrud) SelectById(ctx context.Context, model interface{}, datasrc string, id uint64) (map[string]interface{}, error) {
	return nil, nil
}

func (m *fakeDbCrud) SelectByUnique(ctx context.Context, model interface{}, datasrc string, keyGroup string, uids ...any) (map[string]interface{}, error) {
	return nil, nil
}

func (m *fakeDbCrud) SelectByForeign(ctx context.Context, model interface{}, datasrc string, keyGroup string, fids ...any) ([]map[string]interface{}, error) {
	return nil, nil
}

func (m *fakeDbCrud) Insert(ctx context.Context, model interface{}, datasrc string) (uint64, error) {
	return 0, nil
}

func (m *fakeDbCrud) UpdateById(ctx context.Context, model interface{}, datasrc string) (uint64, error) {
	return 0, nil
}

func (m *fakeDbCrud) UpdateByUnique(ctx context.Context, model interface{}, datasrc string, keyGroup string) (uint64, error) {
	return 0, nil
}

func (m *fakeDbCrud) UpdateByForeign(ctx context.Context, model interface{}, datasrc string, keyGroup string) (uint64, error) {
	return 0, nil
}

func (m *fakeDbCrud) Delete(ctx context.Context, model interface{}, datasrc string, filters []sqldataenums.Filter) (uint64, error) {
	return 0, nil
}

func (m *fakeDbCrud) DeleteById(ctx context.Context, model interface{}, datasrc string, id uint64) (uint64, error) {
	return 0, nil
}

func (m *fakeDbCrud) DeleteByUnique(ctx context.Context, model interface{}, datasrc string, keyGroup string, uids ...any) (uint64, error) {
	return 0, nil
}

func (m *fakeDbCrud) DeleteByForeign(ctx context.Context, model interface{}, datasrc string, keyGroup string, fids ...any) (uint64, error) {
	return 0, nil
}

func (m *fakeDbCrud) Ping(ctx context.Context) error {
	return m.pingErr
}

func (m *fakeDbCrud) BeginTx(ctx context.Context) error {
	return nil
}

func (m *fakeDbCrud) RollbackTx() error {
	return nil
}

func (m *fakeDbCrud) CommitTx() error {
	return nil
}

type fakeApiEndpointRbacService struct{}

func (m *fakeApiEndpointRbacService) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.ApiEndpointRbac, uint64, error) {
	return nil, 0, nil
}

func (m *fakeApiEndpointRbacService) GetApiEpByUserRole(ctx context.Context, userId uint64) ([]*entities.ApiEndpointRbacJoinModel, uint64, error) {
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

func TestHealthCheckHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	HealthCheckHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "alive") {
		t.Fatalf("expected health response body, got %s", rr.Body.String())
	}
}

func TestReadinessCheckHandler(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rr := httptest.NewRecorder()
		handler := ReadinessCheckHandler(&fakeDbCrud{})

		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("not ready", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		rr := httptest.NewRecorder()
		handler := ReadinessCheckHandler(&fakeDbCrud{pingErr: errors.New("db down")})

		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rr.Code)
		}
	})
}

func TestProtectedEndpointIntegration(t *testing.T) {
	router := mux.NewRouter()
	api := router.PathPrefix("/api").Subrouter()

	auth := middlewares.NewAuth("test-secret")
	rbac := middlewares.NewRbac(&fakeApiEndpointRbacService{}, cache.NewMemoryStore(time.Minute, time.Minute), time.Minute)
	apis.NewAdminApi(api, *auth, *rbac)

	token, err := auth.JwtToken(models.JwtCustomClaims{
		Id:     1,
		Email:  "tester@example.com",
		Name:   "tester",
		RoleId: 1,
	})
	if err != nil {
		t.Fatalf("failed to build token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/api/admin/test", nil)
	req.Host = "example.com"
	req.AddCookie(&http.Cookie{Name: middlewares.AuthCookieNameForRequest(req), Value: token})
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Welcome") {
		t.Fatalf("expected welcome message, got %s", rr.Body.String())
	}
}
