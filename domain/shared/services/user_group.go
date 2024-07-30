package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// userGroupService struct
type userGroupService struct {
	dbCrud   dbsql.IDbCrud
	userRepo dbsql.IGenericRepo[entities.UserGroup]
}

// Create new IUserGroupService
func NewUserGroupService(dbCrud dbsql.IDbCrud) IUserGroupService {
	return &userGroupService{
		dbCrud:   dbCrud,
		userRepo: dbsql.NewGenericRepo[entities.UserGroup](dbCrud),
	}
}

func (m *userGroupService) Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.UserGroup, uint64, error) {
	sorters := []sqldataenums.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	return m.userRepo.Get(ctx, limit, offset, nil, sorters, "")
}

func (m *userGroupService) Create(ctx context.Context, model entities.UserGroup) (uint64, error) {
	return m.userRepo.Create(ctx, "", model)
}

func (m *userGroupService) Update(ctx context.Context, model entities.UserGroup) (uint64, error) {
	return m.userRepo.UpdateById(ctx, "", model)
}

func (m *userGroupService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.userRepo.DeleteById(ctx, "", id)
}
