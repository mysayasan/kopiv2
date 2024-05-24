package repos

import (
	"context"

	"github.com/mysayasan/kopiv2/apps/mypropsan/entity"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// IIResidentPropRepo interface
type IResidentPropRepo interface {
	GetLatest(ctx context.Context, limit uint64, offset uint64, filters []dbsql.Filter, sorter []dbsql.Sorter) ([]*models.ResidentPropModel, uint64, error)
}

// IIFileStorageRepo interface
type IFileStorageRepo interface {
	GetByGuid(ctx context.Context, guid string) (*entity.FileStorageEntity, error)
	Add(ctx context.Context, model entity.FileStorageEntity) (uint64, error)
	AddMultiple(ctx context.Context, model []entity.FileStorageEntity) (uint64, error)
}
