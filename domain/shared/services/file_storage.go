package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/infra/cache"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fileStorageService struct
type fileStorageService struct {
	repo  dbsql.IGenericRepo[entities.FileStorage]
	cache cache.Store
}

// Create new IFileStorageService
func NewFileStorageService(
	repo dbsql.IGenericRepo[entities.FileStorage],
	cacheStore cache.Store,
) IFileStorageService {
	return &fileStorageService{
		repo:  repo,
		cache: cacheStore,
	}
}

func (m *fileStorageService) GetByGuid(ctx context.Context, guid string) (*entities.FileStorage, error) {
	return m.repo.GetByUnique(ctx, "", guid)
}

func (m *fileStorageService) Create(ctx context.Context, model entities.FileStorage) (uint64, error) {
	return m.repo.Create(ctx, "", model)
}

func (m *fileStorageService) CreateMultiple(ctx context.Context, model []entities.FileStorage) (uint64, error) {
	return m.repo.CreateMultiple(ctx, "", model)
}
