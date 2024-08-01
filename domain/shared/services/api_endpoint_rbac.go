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
	dbCrud    dbsql.IDbCrud
	repo      dbsql.IGenericRepo[entities.ApiEndpointRbac]
	apiEpRepo dbsql.IGenericRepo[entities.ApiEndpoint]
}

// Create new IApiEndpointRbacService
func NewApiEndpointRbacService(dbCrud dbsql.IDbCrud) IApiEndpointRbacService {
	return &apiEndpointRbacService{
		dbCrud:    dbCrud,
		repo:      dbsql.NewGenericRepo[entities.ApiEndpointRbac](dbCrud),
		apiEpRepo: dbsql.NewGenericRepo[entities.ApiEndpoint](dbCrud),
	}
}

func (m *apiEndpointRbacService) Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiEndpointRbac, uint64, error) {
	sorters := []sqldataenums.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	return m.repo.Get(ctx, limit, offset, nil, sorters, "")
}

func (m *apiEndpointRbacService) Create(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error) {
	return m.repo.Create(ctx, "", model)
}

func (m *apiEndpointRbacService) Update(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error) {
	return m.repo.UpdateById(ctx, "", model)
}

func (m *apiEndpointRbacService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.repo.DeleteById(ctx, "", id)
}

func (m *apiEndpointRbacService) Validate(ctx context.Context, host string, path string, userRoleId uint64) (*entities.ApiEndpointRbac, error) {
	apiEp, err := m.apiEpRepo.GetByUnique(ctx, "", "ukey1", host, path)
	if err != nil {
		return nil, err
	}

	return m.repo.GetByUnique(ctx, "", "ukey1", apiEp.Id, userRoleId)
}
