package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	goCache "github.com/patrickmn/go-cache"
)

// apiEndpointRbacService struct
type apiEndpointRbacService struct {
	repo          dbsql.IGenericRepo[entities.ApiEndpointRbac]
	userLoginRepo dbsql.IGenericRepo[entities.UserLogin]
	apiEpRepo     dbsql.IGenericRepo[entities.ApiEndpoint]
	memCache      *goCache.Cache
}

// Create new IApiEndpointRbacService
func NewApiEndpointRbacService(
	repo dbsql.IGenericRepo[entities.ApiEndpointRbac],
	userLoginRepo dbsql.IGenericRepo[entities.UserLogin],
	apiEpRepo dbsql.IGenericRepo[entities.ApiEndpoint],
	memCache *goCache.Cache,
) IApiEndpointRbacService {
	return &apiEndpointRbacService{
		repo:          repo,
		userLoginRepo: userLoginRepo,
		apiEpRepo:     apiEpRepo,
		memCache:      memCache,
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

func (m *apiEndpointRbacService) GetApiEpByUserRole(ctx context.Context, userId uint64) ([]*entities.ApiEndpointRbacJoinModel, uint64, error) {
	userData, err := m.userLoginRepo.GetById(ctx, "", userId)
	if err != nil {
		return nil, 0, err
	}

	sorters := []sqldataenums.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	filters := []sqldataenums.Filter{
		{
			FieldName: "UserRoleId",
			Value:     userData.UserRoleId,
			Compare:   sqldataenums.Equal,
		},
	}

	data, total, err := m.repo.GetJoin(ctx, "", entities.ApiEndpointRbacJoinModel{}, 0, 0, filters, sorters, "api_endpoint")
	if err != nil {
		return nil, 0, err
	}

	res := make([]*entities.ApiEndpointRbacJoinModel, 0)
	for _, row := range data {
		row := row
		var model entities.ApiEndpointRbacJoinModel
		mapstructure.Decode(row, &model)
		res = append(res, &model)
	}

	return res, total, nil
}
