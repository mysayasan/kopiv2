package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	memCache "github.com/patrickmn/go-cache"
)

// userLoginService struct
type userLoginService struct {
	repo     dbsql.IGenericRepo[entities.UserLogin]
	memCache *memCache.Cache
}

// Create new IUserLoginService
func NewUserLoginService(
	repo dbsql.IGenericRepo[entities.UserLogin],
	memCache *memCache.Cache,
) IUserLoginService {
	return &userLoginService{
		repo:     repo,
		memCache: memCache,
	}
}

func (m *userLoginService) Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.UserLogin, uint64, error) {
	sorters := []sqldataenums.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	return m.repo.Get(ctx, "", limit, offset, nil, sorters)
}

func (m *userLoginService) GetByEmail(ctx context.Context, email string) (*entities.UserLogin, error) {
	return m.repo.GetByUnique(ctx, "", "email", email)
}

func (m *userLoginService) Create(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return m.repo.Create(ctx, "", model)
}

func (m *userLoginService) Update(ctx context.Context, model entities.UserLogin) (uint64, error) {
	return m.repo.UpdateById(ctx, "", model)
}

func (m *userLoginService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.repo.DeleteById(ctx, "", id)
}
