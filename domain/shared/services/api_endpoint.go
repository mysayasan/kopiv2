package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/cache"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// apiEndpointService struct
type apiEndpointService struct {
	repo  dbsql.IGenericRepo[entities.ApiEndpoint]
	cache cache.Store
}

// Create new IApiEndpointService
func NewApiEndpointService(
	repo dbsql.IGenericRepo[entities.ApiEndpoint],
	cacheStore cache.Store,
) IApiEndpointService {
	return &apiEndpointService{
		repo:  repo,
		cache: cacheStore,
	}
}

func (m *apiEndpointService) Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiEndpoint, uint64, error) {
	sorters := []sqldataenums.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	return m.repo.Get(ctx, "", limit, offset, nil, sorters)
}

func (m *apiEndpointService) Create(ctx context.Context, model entities.ApiEndpoint) (uint64, error) {
	return m.repo.Create(ctx, "", model)
}

func (m *apiEndpointService) Update(ctx context.Context, model entities.ApiEndpoint) (uint64, error) {
	return m.repo.UpdateById(ctx, "", model)
}

func (m *apiEndpointService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.repo.DeleteById(ctx, "", id)
}
