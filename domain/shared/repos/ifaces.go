package repos

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/enums/data"
)

// IUserRepo interface
type IUserRepo interface {
	GetAll(ctx context.Context, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter) ([]*entities.UserLoginEntity, uint64, error)
	GetByEmail(ctx context.Context, email string) (*entities.UserLoginEntity, error)
	Create(ctx context.Context, model entities.UserLoginEntity) (uint64, error)
	Update(ctx context.Context, model entities.UserLoginEntity) (uint64, error)
}

// IApiLogRepo interface
type IApiLogRepo interface {
	GetAll(ctx context.Context, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter) ([]*entities.ApiLogEntity, uint64, error)
	Create(ctx context.Context, model entities.ApiLogEntity) (uint64, error)
}
