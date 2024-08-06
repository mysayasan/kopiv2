package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
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

	return m.repo.Get(ctx, "", limit, offset, nil, sorters)
}

func (m *apiEndpointRbacService) GetApiEpByUserRole(ctx context.Context, userRoleId uint64) ([]*entities.ApiEndpoint, error) {
	rbacData, err := m.repo.GetByForeign(ctx, "", "fkey2", userRoleId)
	if err != nil {
		return nil, err
	}

	res := make([]*entities.ApiEndpoint, 0)

	for _, rbac := range rbacData {
		rbac := rbac
		ep, err := m.apiEpRepo.GetById(ctx, "", uint64(rbac.ApiEndpointId))
		if err == nil {
			res = append(res, ep)
		}
	}

	return res, nil
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

func (m *apiEndpointRbacService) GetView(ctx context.Context, userRoleId uint64) ([]*entities.ApiEndpointRbacVwModel, uint64, error) {
	sorters := []sqldataenums.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	filters := []sqldataenums.Filter{
		{
			FieldName: "UserRoleId",
			Value:     userRoleId,
			Compare:   sqldataenums.Equal,
		},
	}

	data, total, err := m.repo.GetJoin(ctx, "", entities.ApiEndpointRbacVwModel{}, 0, 0, filters, sorters, "api_endpoint")
	if err != nil {
		return nil, 0, err
	}

	res := make([]*entities.ApiEndpointRbacVwModel, 0)
	for _, row := range data {
		row := row
		var model entities.ApiEndpointRbacVwModel
		mapstructure.Decode(row, &model)
		res = append(res, &model)
	}

	return res, total, nil
}
