package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

// IUserLoginService interface
type IUserLoginService interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.UserLogin, uint64, error)
	GetByEmail(ctx context.Context, email string) (*entities.UserLogin, error)
	AuthenticateDefault(ctx context.Context, username string, password string) (*entities.UserLogin, error)
	RegisterLocal(ctx context.Context, model entities.UserLogin) (uint64, error)
	Create(ctx context.Context, model entities.UserLogin) (uint64, error)
	Update(ctx context.Context, model entities.UserLogin) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}

// IUserGroupService interface
type IUserGroupService interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.UserGroup, uint64, error)
	Create(ctx context.Context, model entities.UserGroup) (uint64, error)
	Update(ctx context.Context, model entities.UserGroup) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}

// IUserRoleService interface
type IUserRoleService interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.UserRole, uint64, error)
	GetByGroup(ctx context.Context, groupId uint64) ([]*entities.UserRole, error)
	Create(ctx context.Context, model entities.UserRole) (uint64, error)
	Update(ctx context.Context, model entities.UserRole) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}
