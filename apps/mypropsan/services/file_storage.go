package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/apps/mypropsan/entities"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// fileStorageService struct
type fileStorageService struct {
	dbCrud dbsql.IDbCrud
	fsRepo dbsql.IGenericRepo[entities.FileStorage]
}

// Create new IFileStorageService
func NewFileStorageService(dbCrud dbsql.IDbCrud) IFileStorageService {
	return &fileStorageService{
		dbCrud: dbCrud,
		fsRepo: dbsql.NewGenericRepo[entities.FileStorage](dbCrud),
	}
}

func (m *fileStorageService) GetByGuid(ctx context.Context, guid string) (*entities.FileStorage, error) {
	return m.fsRepo.ReadByUKey(ctx, guid)
}

func (m *fileStorageService) Create(ctx context.Context, model entities.FileStorage) (uint64, error) {
	return m.fsRepo.Create(ctx, model)
}

func (m *fileStorageService) CreateMultiple(ctx context.Context, model []entities.FileStorage) (uint64, error) {
	return m.fsRepo.CreateMultiple(ctx, model)
}
