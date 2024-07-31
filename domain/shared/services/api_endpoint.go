package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// apiEndpointService struct
type apiEndpointService struct {
	dbCrud   dbsql.IDbCrud
	userRepo dbsql.IGenericRepo[entities.ApiEndpoint]
}

// Create new IApiEndpointService
func NewApiEndpointService(dbCrud dbsql.IDbCrud) IApiEndpointService {
	return &apiEndpointService{
		dbCrud:   dbCrud,
		userRepo: dbsql.NewGenericRepo[entities.ApiEndpoint](dbCrud),
	}
}

func (m *apiEndpointService) Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiEndpoint, uint64, error) {
	sorters := []sqldataenums.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	return m.userRepo.Get(ctx, limit, offset, nil, sorters, "")
}

func (m *apiEndpointService) Create(ctx context.Context, model entities.ApiEndpoint) (uint64, error) {
	return m.userRepo.Create(ctx, "", model)
}

func (m *apiEndpointService) Update(ctx context.Context, model entities.ApiEndpoint) (uint64, error) {
	return m.userRepo.UpdateById(ctx, "", model)
}

func (m *apiEndpointService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.userRepo.DeleteById(ctx, "", id)
}
