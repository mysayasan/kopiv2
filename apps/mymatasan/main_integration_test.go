package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

type fakeDbCrud struct {
	pingErr error
}

func (m *fakeDbCrud) Select(ctx context.Context, model interface{}, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter, datasrc string, joinsrc ...string) ([]map[string]interface{}, uint64, error) {
	return nil, 0, nil
}

func (m *fakeDbCrud) SelectJoin(ctx context.Context, model interface{}, limit uint64, offset uint64, filters []sqldataenums.Filter, sorter []sqldataenums.Sorter, datasrc string, joins ...dbsql.JoinSpec) ([]map[string]interface{}, uint64, error) {
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
