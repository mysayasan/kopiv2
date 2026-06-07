package apis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	outputdtos "github.com/mysayasan/kopiv2/domain/shared/dtos/output"
)

type fakeUserLoginListService struct {
	lastLimit   uint64
	lastOffset  uint64
	lastFilters []sqldataenums.Filter
	lastSorters []sqldataenums.Sorter
}

func (f *fakeUserLoginListService) Get(_ context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*outputdtos.UserLoginDto, uint64, error) {
	f.lastLimit = limit
	f.lastOffset = offset
	f.lastFilters = filters
	f.lastSorters = sorters
	return []*outputdtos.UserLoginDto{{Id: 7, Email: "active@example.test", IsActive: true}}, 1, nil
}

func (f *fakeUserLoginListService) GetByEmail(context.Context, string) (*outputdtos.UserLoginDto, error) {
	return nil, nil
}

func (f *fakeUserLoginListService) AuthenticateDefault(context.Context, string, string) (*outputdtos.UserLoginDto, error) {
	return nil, nil
}

func (f *fakeUserLoginListService) RegisterLocal(context.Context, entities.UserLogin) (uint64, error) {
	return 0, nil
}

func (f *fakeUserLoginListService) Create(context.Context, entities.UserLogin) (uint64, error) {
	return 0, nil
}

func (f *fakeUserLoginListService) Update(context.Context, entities.UserLogin) (uint64, error) {
	return 0, nil
}

func (f *fakeUserLoginListService) Delete(context.Context, uint64) (uint64, error) {
	return 0, nil
}

func TestUserLoginGetAppliesFiltersAndSorters(t *testing.T) {
	service := &fakeUserLoginListService{}
	api := &userLoginApi{serv: service}
	target := "/api/user-credential?limit=10&offset=20"
	target += "&filters=" + url.QueryEscape(`[{"fieldName":"isActive","compare":1,"value":true}]`)
	target += "&sorters=" + url.QueryEscape(`[{"fieldName":"email","sort":1}]`)

	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	api.get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if service.lastLimit != 10 || service.lastOffset != 20 {
		t.Fatalf("unexpected paging: limit=%d offset=%d", service.lastLimit, service.lastOffset)
	}
	if len(service.lastFilters) != 1 {
		t.Fatalf("expected one filter, got %d", len(service.lastFilters))
	}
	if service.lastFilters[0].FieldName != "IsActive" || service.lastFilters[0].Compare != sqldataenums.Equal || service.lastFilters[0].Value != true {
		t.Fatalf("unexpected filter: %#v", service.lastFilters[0])
	}
	if len(service.lastSorters) != 1 {
		t.Fatalf("expected one sorter, got %d", len(service.lastSorters))
	}
	if service.lastSorters[0].FieldName != "Email" || service.lastSorters[0].Sort != sqldataenums.ASC {
		t.Fatalf("unexpected sorter: %#v", service.lastSorters[0])
	}
}
