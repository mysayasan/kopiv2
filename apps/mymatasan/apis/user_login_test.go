package apis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	outputdtos "github.com/mysayasan/kopiv2/apps/mymatasan/dtos/output"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

type fakeUserLoginService struct {
	users   []*outputdtos.UserLoginDto
	user    *outputdtos.UserLoginDto
	limit   uint64
	offset  uint64
	filters []sqldataenums.Filter
	sorters []sqldataenums.Sorter
	email   string
}

func (m *fakeUserLoginService) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*outputdtos.UserLoginDto, uint64, error) {
	m.limit = limit
	m.offset = offset
	m.filters = filters
	m.sorters = sorters
	return m.users, uint64(len(m.users)), nil
}

func (m *fakeUserLoginService) GetByEmail(ctx context.Context, email string) (*outputdtos.UserLoginDto, error) {
	m.email = email
	return m.user, nil
}

func (m *fakeUserLoginService) AuthenticateDefault(ctx context.Context, username string, password string) (*outputdtos.UserLoginDto, error) {
	return nil, nil
}

func (m *fakeUserLoginService) RegisterLocal(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return 0, nil
}

func (m *fakeUserLoginService) Create(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return 0, nil
}

func (m *fakeUserLoginService) Update(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return 0, nil
}

func (m *fakeUserLoginService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return 0, nil
}

func TestUserLoginApiGetReturnsPasswordlessDTOs(t *testing.T) {
	service := &fakeUserLoginService{
		users: []*outputdtos.UserLoginDto{{
			Id:    1,
			Email: "admin@example.test",
		}},
	}
	api := &userLoginApi{serv: service}
	query := url.Values{}
	query.Set("limit", "10")
	query.Set("offset", "5")
	query.Set("filters", `[{"fieldName":"createdAt","compare":5,"value":1700000000}]`)
	query.Set("sorters", `[{"fieldName":"createdAt","sort":2}]`)
	req := httptest.NewRequest(http.MethodGet, "/api/user-login?"+query.Encode(), nil)
	rr := httptest.NewRecorder()

	api.get(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rr.Code, rr.Body.String())
	}
	if service.limit != 10 || service.offset != 5 {
		t.Fatalf("paging not forwarded limit=%d offset=%d", service.limit, service.offset)
	}
	if len(service.filters) != 1 || service.filters[0].FieldName != "CreatedAt" {
		t.Fatalf("filter not forwarded: %+v", service.filters)
	}
	if len(service.sorters) != 1 || service.sorters[0].FieldName != "CreatedAt" {
		t.Fatalf("sorter not forwarded: %+v", service.sorters)
	}

	var body any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response json: %v", err)
	}
	if containsJSONFieldValue(body, "userpwd") {
		t.Fatalf("password field leaked in api response: %s", rr.Body.String())
	}
}

func TestUserLoginApiGetByEmailReturnsPasswordlessDTO(t *testing.T) {
	service := &fakeUserLoginService{
		user: &outputdtos.UserLoginDto{
			Id:    2,
			Email: "person@example.test",
		},
	}
	api := &userLoginApi{serv: service}
	req := httptest.NewRequest(http.MethodGet, "/api/user-login/email?email=person%40example.test", nil)
	rr := httptest.NewRecorder()

	api.getByEmail(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rr.Code, rr.Body.String())
	}
	if service.email != "person@example.test" {
		t.Fatalf("email not forwarded: %q", service.email)
	}

	var body any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response json: %v", err)
	}
	if containsJSONFieldValue(body, "userpwd") {
		t.Fatalf("password field leaked in api response: %s", rr.Body.String())
	}
}

func containsJSONFieldValue(value any, field string) bool {
	switch v := value.(type) {
	case map[string]any:
		if _, ok := v[field]; ok {
			return true
		}
		for _, child := range v {
			if containsJSONFieldValue(child, field) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsJSONFieldValue(child, field) {
				return true
			}
		}
	}
	return false
}
