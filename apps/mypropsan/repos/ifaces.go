package repos

import (
	"context"

	"github.com/mysayasan/kopiv2/apps/mypropsan/entities"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	"github.com/mysayasan/kopiv2/domain/enums/data"
)

// IResidentPropRepo interface
type IResidentPropRepo interface {
	GetLatest(ctx context.Context, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter) ([]*models.ResidentProp, uint64, error)
}

// IFileStorageRepo interface
type IFileStorageRepo interface {
	GetByGuid(ctx context.Context, guid string) (*entities.FileStorage, error)
	Create(ctx context.Context, model entities.FileStorage) (uint64, error)
	CreateMultiple(ctx context.Context, model []entities.FileStorage) (uint64, error)
}
