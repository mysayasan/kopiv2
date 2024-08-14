package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fileStorageService struct
type fileStorageService struct {
	repo dbsql.IGenericRepo[entities.FileStorage]
}

// Create new IFileStorageService
func NewFileStorageService(repo dbsql.IGenericRepo[entities.FileStorage]) IFileStorageService {
	return &fileStorageService{
		repo: repo,
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
