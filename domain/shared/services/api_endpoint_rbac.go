package services

import (
	"context"
	"fmt"
	"log"

	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	"github.com/mysayasan/kopiv2/domain/entities"
	memcacheenums "github.com/mysayasan/kopiv2/domain/enums/memcache"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/cache"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// apiEndpointRbacService struct
type apiEndpointRbacService struct {
	repo          dbsql.IGenericRepo[entities.ApiEndpointRbac]
	userLoginRepo dbsql.IGenericRepo[entities.UserLogin]
	apiEpRepo     dbsql.IGenericRepo[entities.ApiEndpoint]
	cache         cache.Store
}

// Create new IApiEndpointRbacService
func NewApiEndpointRbacService(
	repo dbsql.IGenericRepo[entities.ApiEndpointRbac],
	userLoginRepo dbsql.IGenericRepo[entities.UserLogin],
	apiEpRepo dbsql.IGenericRepo[entities.ApiEndpoint],
	cacheStore cache.Store,
) IApiEndpointRbacService {
	return &apiEndpointRbacService{
		repo:          repo,
		userLoginRepo: userLoginRepo,
		apiEpRepo:     apiEpRepo,
		cache:         cacheStore,
	}
}

func (m *apiEndpointRbacService) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.ApiEndpointRbac, uint64, error) {
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

func (m *apiEndpointRbacService) Create(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error) {
	id, err := m.repo.Create(ctx, "", model)
	if err != nil {
		return 0, err
	}
	if err := m.invalidateRbacAccessCache(ctx); err != nil {
		log.Printf("rbac cache invalidation warning after create: %v", err)
	}
	return id, nil
}

func (m *apiEndpointRbacService) Update(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error) {
	id, err := m.repo.UpdateById(ctx, "", model)
	if err != nil {
		return 0, err
	}
	if err := m.invalidateRbacAccessCache(ctx); err != nil {
		log.Printf("rbac cache invalidation warning after update: %v", err)
	}
	return id, nil
}

func (m *apiEndpointRbacService) Delete(ctx context.Context, id uint64) (uint64, error) {
	deleted, err := m.repo.DeleteById(ctx, "", id)
	if err != nil {
		return 0, err
	}
	if err := m.invalidateRbacAccessCache(ctx); err != nil {
		log.Printf("rbac cache invalidation warning after delete: %v", err)
	}
	return deleted, nil
}

func (m *apiEndpointRbacService) Validate(ctx context.Context, host string, path string, userRoleId uint64) (*entities.ApiEndpointRbac, error) {
	apiEp, err := m.getEndpointByHostPath(ctx, host, path)
	if err != nil {
		apiEp, err = m.getEndpointByHostPath(ctx, "*", path)
		if err != nil {
			return nil, err
		}
	}

	return m.repo.GetByUnique(ctx, "", "ukey1", apiEp.Id, userRoleId)
}

func (m *apiEndpointRbacService) getEndpointByHostPath(ctx context.Context, host string, path string) (*entities.ApiEndpoint, error) {
	filters := []sqldataenums.Filter{
		{
			FieldName: "Host",
			Value:     host,
			Compare:   sqldataenums.Equal,
		},
		{
			FieldName: "Path",
			Value:     path,
			Compare:   sqldataenums.Equal,
		},
	}
	endpoints, _, err := m.apiEpRepo.Get(ctx, "", 1, 0, filters, nil)
	if err != nil {
		return nil, err
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("api endpoint not found")
	}
	return endpoints[0], nil
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

func (m *apiEndpointRbacService) invalidateRbacAccessCache(ctx context.Context) error {
	prefix := memcacheenums.GetString(memcacheenums.Mware_Rbac_GetApiEpByUserRole_Result) + ":"
	return m.cache.DeleteByPrefix(ctx, prefix)
}
