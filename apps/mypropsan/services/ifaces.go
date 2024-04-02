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

// IStorageService interface
type IStorageService interface {
	GetByGuid(ctx context.Context, guid string) (*entity.StorageEntity, error)
	Add(ctx context.Context, model entity.StorageEntity) (uint64, error)
}
