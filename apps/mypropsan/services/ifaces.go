package services

import (
	"context"

	"github.com/mysayasan/kopiv2/apps/mypropsan/entity"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
)

// IHomeService interface
type IHomeService interface {
	GetLatest(ctx context.Context, limit uint64, offset uint64) ([]*models.ResidentPropModel, uint64, error)
}

// IFileStorageService interface
type IFileStorageService interface {
	GetByGuid(ctx context.Context, guid string) (*entity.FileStorageEntity, error)
	Add(ctx context.Context, model entity.FileStorageEntity) (uint64, error)
	AddMultiple(ctx context.Context, model []entity.FileStorageEntity) (uint64, error)
}
