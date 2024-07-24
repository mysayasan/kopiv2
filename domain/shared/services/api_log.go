package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// apiLogService struct
type apiLogService struct {
	apiRepo dbsql.IGenericRepo[entities.ApiLog]
}

// Create new IApiLogService
func NewApiLogService(dbCrud dbsql.IDbCrud) IApiLogService {
	return &apiLogService{
		apiRepo: dbsql.NewGenericRepo[entities.ApiLog](dbCrud),
	}
}

func (m *apiLogService) Read(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiLog, uint64, error) {
	sorters := []sqldataenums.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	return m.apiRepo.Read(ctx, limit, offset, nil, sorters, "")
}

func (m *apiLogService) Create(ctx context.Context, model entities.ApiLog) (uint64, error) {
	return m.apiRepo.Create(ctx, model)
}
