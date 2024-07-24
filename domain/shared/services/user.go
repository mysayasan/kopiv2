package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// userService struct
type userService struct {
	dbCrud   dbsql.IDbCrud
	userRepo dbsql.IGenericRepo[entities.UserLogin]
}

// Create new IUserService
func NewUserService(dbCrud dbsql.IDbCrud) IUserService {
	return &userService{
		dbCrud:   dbCrud,
		userRepo: dbsql.NewGenericRepo[entities.UserLogin](dbCrud),
	}
}

func (m *userService) Read(ctx context.Context, limit uint64, offset uint64) ([]*entities.UserLogin, uint64, error) {
	sorters := []sqldataenums.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	return m.userRepo.Read(ctx, limit, offset, nil, sorters, "")
}

func (m *userService) ReadByEmail(ctx context.Context, email string) (*entities.UserLogin, error) {
	return m.userRepo.ReadByUnique(ctx, email)
}

func (m *userService) Create(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return m.userRepo.Create(ctx, model)
}

func (m *userService) Update(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return m.userRepo.UpdateById(ctx, model)
}

func (m *userService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.userRepo.DeleteById(ctx, id)
}
