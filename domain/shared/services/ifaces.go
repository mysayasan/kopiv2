package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
)

// IUserService interface
type IUserService interface {
	GetAll(ctx context.Context, limit uint64, offset uint64) ([]*entities.UserLogin, uint64, error)
	GetByEmail(ctx context.Context, email string) (*entities.UserLogin, error)
	Create(ctx context.Context, model entities.UserLogin) (uint64, error)
	Update(ctx context.Context, model entities.UserLogin) (uint64, error)
	Delete(ctx context.Context, model entities.UserLogin) (uint64, error)
}

// IApiLogService interface
type IApiLogService interface {
	GetAll(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiLog, uint64, error)
	Create(ctx context.Context, model entities.ApiLog) (uint64, error)
}
