package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
)

// IUserService interface
type IUserService interface {
	GetAll(ctx context.Context, limit uint64, offset uint64) ([]*entities.UserLoginEntity, uint64, error)
	GetByEmail(ctx context.Context, email string) (*entities.UserLoginEntity, error)
	Create(ctx context.Context, model entities.UserLoginEntity) (uint64, error)
	Update(ctx context.Context, model entities.UserLoginEntity) (uint64, error)
}

// IApiLogService interface
type IApiLogService interface {
	GetAll(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiLogEntity, uint64, error)
	Create(ctx context.Context, model entities.ApiLogEntity) (uint64, error)
}
