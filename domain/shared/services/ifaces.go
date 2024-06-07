package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
)

// IUserService interface
type IUserService interface {
	GetAll(ctx context.Context, limit uint64, offset uint64) ([]*entities.UserLoginEntity, uint64, error)
	GetByEmail(ctx context.Context, email string) (*entities.UserLoginEntity, error)
	Add(ctx context.Context, model entities.UserLoginEntity) (uint64, error)
}
