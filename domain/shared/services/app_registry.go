package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/cache"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

type appRegistryService struct {
	repo  dbsql.IGenericRepo[entities.AppRegistry]
	cache cache.Store
}

func NewAppRegistryService(
	repo dbsql.IGenericRepo[entities.AppRegistry],
	cacheStore cache.Store,
) IAppRegistryService {
	return &appRegistryService{
		repo:  repo,
		cache: cacheStore,
	}
}

func (m *appRegistryService) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.AppRegistry, uint64, error) {
	if len(sorters) == 0 {
		sorters = []sqldataenums.Sorter{
			{
				FieldName: "CreatedAt",
				Sort:      sqldataenums.DESC,
			},
		}
	}

	return m.repo.Get(ctx, "", limit, offset, filters, sorters)
}

func (m *appRegistryService) Create(ctx context.Context, model entities.AppRegistry) (uint64, error) {
	return m.repo.Create(ctx, "", model)
}

func (m *appRegistryService) Update(ctx context.Context, model entities.AppRegistry) (uint64, error) {
	return m.repo.UpdateById(ctx, "", model)
}

func (m *appRegistryService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.repo.DeleteById(ctx, "", id)
}
