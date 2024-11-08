package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	memCache "github.com/patrickmn/go-cache"
)

// apiEndpointService struct
type apiEndpointService struct {
	repo     dbsql.IGenericRepo[entities.ApiEndpoint]
	memCache *memCache.Cache
}

// Create new IApiEndpointService
func NewApiEndpointService(
	repo dbsql.IGenericRepo[entities.ApiEndpoint],
	memCache *memCache.Cache,
) IApiEndpointService {
	return &apiEndpointService{
		repo:     repo,
		memCache: memCache,
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
