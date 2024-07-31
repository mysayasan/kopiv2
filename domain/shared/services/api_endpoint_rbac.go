package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// apiEndpointRbacService struct
type apiEndpointRbacService struct {
	dbCrud   dbsql.IDbCrud
	userRepo dbsql.IGenericRepo[entities.ApiEndpointRbac]
}

// Create new IApiEndpointRbacService
func NewApiEndpointRbacService(dbCrud dbsql.IDbCrud) IApiEndpointRbacService {
	return &apiEndpointRbacService{
		dbCrud:   dbCrud,
		userRepo: dbsql.NewGenericRepo[entities.ApiEndpointRbac](dbCrud),
	}
}

func (m *apiEndpointRbacService) Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiEndpointRbac, uint64, error) {
	sorters := []sqldataenums.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	return m.userRepo.Get(ctx, limit, offset, nil, sorters, "")
}

func (m *apiEndpointRbacService) Create(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error) {
	return m.userRepo.Create(ctx, "", model)
}

func (m *apiEndpointRbacService) Update(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error) {
	return m.userRepo.UpdateById(ctx, "", model)
}

func (m *apiEndpointRbacService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.userRepo.DeleteById(ctx, "", id)
}
