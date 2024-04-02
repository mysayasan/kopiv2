package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/apps/mypropsan/entity"
	"github.com/mysayasan/kopiv2/apps/mypropsan/repos"
)

// storageService struct
type storageService struct {
	repo repos.IStorageRepo
}

// Create new IStorageService
func NewStorageService(repo repos.IStorageRepo) IStorageService {
	return &storageService{
		repo: repo,
	}
}

func (m *storageService) GetByGuid(ctx context.Context, guid string) (*entity.StorageEntity, error) {
	return m.repo.GetByGuid(ctx, guid)
}

func (m *storageService) Add(ctx context.Context, model entity.StorageEntity) (uint64, error) {
	return m.repo.Add(ctx, model)
}
