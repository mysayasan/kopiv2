package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	memCache "github.com/patrickmn/go-cache"
)

// apiLogService struct
type apiLogService struct {
	repo     dbsql.IGenericRepo[entities.ApiLog]
	memCache *memCache.Cache
}

// Create new IApiLogService
func NewApiLogService(
	repo dbsql.IGenericRepo[entities.ApiLog],
	memCache *memCache.Cache,
) IApiLogService {
	return &apiLogService{
		repo:     repo,
		memCache: memCache,
	}
}

func (m *apiLogService) Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiLog, uint64, error) {
	sorters := []sqldataenums.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	return m.repo.Get(ctx, "", limit, offset, nil, sorters)
}

func (m *apiLogService) Create(ctx context.Context, model entities.ApiLog) (uint64, error) {
	return m.repo.Create(ctx, "", model)
}
