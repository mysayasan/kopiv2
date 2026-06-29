package services

import (
	"context"
	"testing"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

type sharedAdapterDTO struct {
	Id    int64  `json:"id"`
	Title string `json:"title"`
}

type fakeUserGroupCoreService struct {
	users []*entities.UserGroup
}

func (m *fakeUserGroupCoreService) Get(context.Context, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter) ([]*entities.UserGroup, uint64, error) {
	return m.users, uint64(len(m.users)), nil
}

func (m *fakeUserGroupCoreService) Create(context.Context, entities.UserGroup) (uint64, error) {
	return 0, nil
}

func (m *fakeUserGroupCoreService) Update(context.Context, entities.UserGroup) (uint64, error) {
	return 0, nil
}

func (m *fakeUserGroupCoreService) Delete(context.Context, uint64) (uint64, error) {
	return 0, nil
}

func TestUserGroupDtoServiceGetReturnsSuppliedDTO(t *testing.T) {
	service := NewUserGroupDtoService[sharedAdapterDTO](&fakeUserGroupCoreService{
		users: []*entities.UserGroup{{Id: 1, Title: "system"}},
	})

	res, totalCnt, err := service.Get(context.Background(), 10, 0, nil, nil)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if totalCnt != 1 || len(res) != 1 || res[0].Title != "system" {
		t.Fatalf("unexpected dto result total=%d res=%+v", totalCnt, res)
	}
}
