package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	memCache "github.com/patrickmn/go-cache"
)

// userRoleService struct
type userRoleService struct {
	repo     dbsql.IGenericRepo[entities.UserRole]
	memCache *memCache.Cache
}

// Create new IUserRoleService
func NewUserRoleService(
	repo dbsql.IGenericRepo[entities.UserRole],
	memCache *memCache.Cache,
) IUserRoleService {
	return &userRoleService{
		repo:     repo,
		memCache: memCache,
	}
}

func (m *userRoleService) Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.UserRole, uint64, error) {
	sorters := []sqldataenums.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	return m.repo.Get(ctx, "", limit, offset, nil, sorters)
}

// GetByGroup implements IUserRoleService.
func (m *userRoleService) GetByGroup(ctx context.Context, groupId uint64) ([]*entities.UserRole, error) {
	return m.repo.GetByForeign(ctx, "", "group", groupId)
}

func (m *userRoleService) Create(ctx context.Context, model entities.UserRole) (uint64, error) {
	return m.repo.Create(ctx, "", model)
}

func (m *userRoleService) Update(ctx context.Context, model entities.UserRole) (uint64, error) {
	return m.repo.UpdateById(ctx, "", model)
}

func (m *userRoleService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.repo.DeleteById(ctx, "", id)
}
