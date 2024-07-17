package services

import (
	"context"

	"github.com/mysayasan/kopiv2/apps/mypropsan/entities"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
)

// IHomeService interface
type IHomeService interface {
	GetLatest(ctx context.Context, limit uint64, offset uint64) ([]*models.ResidentProp, uint64, error)
}

// IFileStorageService interface
type IFileStorageService interface {
	GetByGuid(ctx context.Context, guid string) (*entities.FileStorage, error)
	Create(ctx context.Context, model entities.FileStorage) (uint64, error)
	CreateMultiple(ctx context.Context, model []entities.FileStorage) (uint64, error)
}
