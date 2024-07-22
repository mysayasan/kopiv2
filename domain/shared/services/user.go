package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/enums/data"
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

func (m *userService) ReadAll(ctx context.Context, limit uint64, offset uint64) ([]*entities.UserLogin, uint64, error) {
	sorters := []data.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	return m.userRepo.ReadAll(ctx, limit, offset, nil, sorters)
}

func (m *userService) GetByEmail(ctx context.Context, email string) (*entities.UserLogin, error) {
	// return m.repo.GetByEmail(ctx, email)
	return nil, nil
}

func (m *userService) Create(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return m.userRepo.Create(ctx, model)
}

func (m *userService) Update(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return m.userRepo.Update(ctx, model)
}

func (m *userService) Delete(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return m.userRepo.Delete(ctx, model)
}
