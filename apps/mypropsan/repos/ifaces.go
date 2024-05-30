package repos

import (
	"context"

	"github.com/mysayasan/kopiv2/apps/mypropsan/entity"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	"github.com/mysayasan/kopiv2/domain/enums/data"
)

// IResidentPropRepo interface
type IResidentPropRepo interface {
	GetLatest(ctx context.Context, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter) ([]*models.ResidentPropModel, uint64, error)
}

// IFileStorageRepo interface
type IFileStorageRepo interface {
	GetByGuid(ctx context.Context, guid string) (*entity.FileStorageEntity, error)
	Add(ctx context.Context, model entity.FileStorageEntity) (uint64, error)
	AddMultiple(ctx context.Context, model []entity.FileStorageEntity) (uint64, error)
}

// IUserRepo interface
type IUserRepo interface {
	GetAll(ctx context.Context, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter) ([]*entity.UserLoginEntity, uint64, error)
	GetByEmail(ctx context.Context, email string) (*entity.UserLoginEntity, error)
	Add(ctx context.Context, model entity.UserLoginEntity) (uint64, error)
}
