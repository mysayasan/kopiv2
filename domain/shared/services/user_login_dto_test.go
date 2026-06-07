package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

type userLoginDTOForTest struct {
	Id        int64  `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
}

type fakeUserLoginCoreService struct {
	users   []*entities.UserLogin
	user    *entities.UserLogin
	limit   uint64
	offset  uint64
	filters []sqldataenums.Filter
	sorters []sqldataenums.Sorter
	email   string
}

func (m *fakeUserLoginCoreService) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.UserLogin, uint64, error) {
	m.limit = limit
	m.offset = offset
	m.filters = filters
	m.sorters = sorters
	return m.users, uint64(len(m.users)), nil
}

func (m *fakeUserLoginCoreService) GetByEmail(ctx context.Context, email string) (*entities.UserLogin, error) {
	m.email = email
	if m.user == nil {
		return nil, errors.New("not found")
	}
	return m.user, nil
}

func (m *fakeUserLoginCoreService) AuthenticateDefault(ctx context.Context, username string, password string) (*entities.UserLogin, error) {
	return nil, nil
}

func (m *fakeUserLoginCoreService) RegisterLocal(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return 0, nil
}

func (m *fakeUserLoginCoreService) Create(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return 0, nil
}

func (m *fakeUserLoginCoreService) Update(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return 0, nil
}

func (m *fakeUserLoginCoreService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return 0, nil
}

func TestUserLoginDtoServiceReturnsSuppliedDTO(t *testing.T) {
	shared := &fakeUserLoginCoreService{
		users: []*entities.UserLogin{{
			Id:        7,
			Email:     "admin@example.test",
			Userpwd:   "hashed-secret",
			FirstName: "Admin",
		}},
	}
	service := NewUserLoginDtoService[userLoginDTOForTest](shared)

	res, totalCnt, err := service.Get(context.Background(), 10, 5, []sqldataenums.Filter{{FieldName: "CreatedAt", Compare: sqldataenums.GreaterThanOrEqualTo, Value: int64(1700000000)}}, []sqldataenums.Sorter{{FieldName: "CreatedAt", Sort: sqldataenums.DESC}})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if totalCnt != 1 || len(res) != 1 {
		t.Fatalf("unexpected result count total=%d len=%d", totalCnt, len(res))
	}
	if shared.limit != 10 || shared.offset != 5 {
		t.Fatalf("paging not forwarded limit=%d offset=%d", shared.limit, shared.offset)
	}
	if len(shared.filters) != 1 || shared.filters[0].FieldName != "CreatedAt" {
		t.Fatalf("filters not forwarded: %+v", shared.filters)
	}
	if len(shared.sorters) != 1 || shared.sorters[0].FieldName != "CreatedAt" {
		t.Fatalf("sorters not forwarded: %+v", shared.sorters)
	}
	if res[0].Email != "admin@example.test" || res[0].FirstName != "Admin" {
		t.Fatalf("unexpected dto: %+v", res[0])
	}

	body, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if containsJSONField(body, "userpwd") {
		t.Fatalf("password field leaked in dto json: %s", body)
	}
}

func TestUserLoginDtoServiceGetByEmailReturnsSuppliedDTO(t *testing.T) {
	shared := &fakeUserLoginCoreService{
		user: &entities.UserLogin{
			Id:      8,
			Email:   "person@example.test",
			Userpwd: "hashed-secret",
		},
	}
	service := NewUserLoginDtoService[userLoginDTOForTest](shared)

	res, err := service.GetByEmail(context.Background(), "person@example.test")
	if err != nil {
		t.Fatalf("GetByEmail failed: %v", err)
	}
	if shared.email != "person@example.test" {
		t.Fatalf("email not forwarded: %q", shared.email)
	}
	if res.Email != "person@example.test" {
		t.Fatalf("unexpected dto: %+v", res)
	}

	body, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if containsJSONField(body, "userpwd") {
		t.Fatalf("password field leaked in dto json: %s", body)
	}
}

func containsJSONField(body []byte, field string) bool {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return false
	}
	return containsJSONFieldValue(decoded, field)
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
