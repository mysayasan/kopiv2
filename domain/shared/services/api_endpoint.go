package services

import (
	"context"
	"log"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	memcacheenums "github.com/mysayasan/kopiv2/domain/enums/memcache"
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
	id, err := m.repo.Create(ctx, "", model)
	if err != nil {
		return 0, err
	}
	m.invalidateAccessCache(ctx, "create")
	return id, nil
}

func (m *apiEndpointService) Update(ctx context.Context, model entities.ApiEndpoint) (uint64, error) {
	id, err := m.repo.UpdateById(ctx, "", model)
	if err != nil {
		return 0, err
	}
	m.invalidateAccessCache(ctx, "update")
	return id, nil
}

func (m *apiEndpointService) Delete(ctx context.Context, id uint64) (uint64, error) {
	deleted, err := m.repo.DeleteById(ctx, "", id)
	if err != nil {
		return 0, err
	}
	m.invalidateAccessCache(ctx, "delete")
	return deleted, nil
}

func (m *apiEndpointService) invalidateAccessCache(ctx context.Context, action string) {
	if m.cache == nil {
		return
	}
	prefix := memcacheenums.GetString(memcacheenums.Mware_Rbac_GetApiEpByUserRole_Result) + ":"
	if err := m.cache.DeleteByPrefix(ctx, prefix); err != nil {
		log.Printf("endpoint cache invalidation warning after %s: %v", action, err)
	}
	if err := m.cache.Delete(ctx, memcacheenums.GetString(memcacheenums.Mware_RateLimit_ApiEndpointTiers)); err != nil {
		log.Printf("endpoint tier cache invalidation warning after %s: %v", action, err)
	}
}
