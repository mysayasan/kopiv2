package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/apps/mypropsan/entity"
	"github.com/mysayasan/kopiv2/apps/mypropsan/repos"
)

// fileStorageService struct
type fileStorageService struct {
	repo repos.IFileStorageRepo
}

// Create new IFileStorageService
func NewFileStorageService(repo repos.IFileStorageRepo) IFileStorageService {
	return &fileStorageService{
		repo: repo,
	}
}

func (m *fileStorageService) GetByGuid(ctx context.Context, guid string) (*entity.FileStorageEntity, error) {
	return m.repo.GetByGuid(ctx, guid)
}

func (m *fileStorageService) Add(ctx context.Context, model entity.FileStorageEntity) (uint64, error) {
	return m.repo.Add(ctx, model)
}

func (m *fileStorageService) AddMultiple(ctx context.Context, model []entity.FileStorageEntity) (uint64, error) {
	return m.repo.AddMultiple(ctx, model)
}
