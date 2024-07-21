package repos

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/enums/data"
)

// IUserRepo interface
type IUserRepo interface {
	GetAll(ctx context.Context, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter) ([]*entities.UserLogin, uint64, error)
	GetByEmail(ctx context.Context, email string) (*entities.UserLogin, error)
	Create(ctx context.Context, model entities.UserLogin) (uint64, error)
	Update(ctx context.Context, model entities.UserLogin) (uint64, error)
	Delete(ctx context.Context, model entities.UserLogin) (uint64, error)
}

// IApiLogRepo interface
type IApiLogRepo interface {
	GetAll(ctx context.Context, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter) ([]*entities.ApiLog, uint64, error)
	Create(ctx context.Context, model entities.ApiLog) (uint64, error)
}

// IRepo interface
type IRepo[T any] interface {
	GetAll(ctx context.Context, limit uint64, offset uint64, filters []data.Filter, sorter []data.Sorter) ([]*T, uint64, error)
	GetByEmail(ctx context.Context, email string) (*T, error)
	Create(ctx context.Context, model T) (uint64, error)
	Update(ctx context.Context, model T) (uint64, error)
	Delete(ctx context.Context, model T) (uint64, error)
}
