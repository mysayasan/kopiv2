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

type fakeUserRoleCoreService struct {
	roles []*entities.UserRole
}

func (m *fakeUserRoleCoreService) Get(context.Context, uint64, uint64, []sqldataenums.Filter, []sqldataenums.Sorter) ([]*entities.UserRole, uint64, error) {
	return m.roles, uint64(len(m.roles)), nil
}

func (m *fakeUserRoleCoreService) GetByGroup(context.Context, uint64) ([]*entities.UserRole, error) {
	return m.roles, nil
}

func (m *fakeUserRoleCoreService) Create(context.Context, entities.UserRole) (uint64, error) {
	return 0, nil
}

func (m *fakeUserRoleCoreService) Update(context.Context, entities.UserRole) (uint64, error) {
	return 0, nil
}

func (m *fakeUserRoleCoreService) Delete(context.Context, uint64) (uint64, error) {
	return 0, nil
}

func TestUserRoleDtoServiceGetReturnsSuppliedDTO(t *testing.T) {
	service := NewUserRoleDtoService[sharedAdapterDTO](&fakeUserRoleCoreService{
		roles: []*entities.UserRole{{Id: 2, Title: "admin"}},
	})

	res, totalCnt, err := service.Get(context.Background(), 10, 0, nil, nil)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if totalCnt != 1 || len(res) != 1 || res[0].Title != "admin" {
		t.Fatalf("unexpected dto result total=%d res=%+v", totalCnt, res)
	}

	byGroup, err := service.GetByGroup(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetByGroup failed: %v", err)
	}
	if len(byGroup) != 1 || byGroup[0].Title != "admin" {
		t.Fatalf("unexpected group dto result: %+v", byGroup)
	}
}
