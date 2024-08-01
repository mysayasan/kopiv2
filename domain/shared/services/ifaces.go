package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
)

// IUserLoginService interface
type IUserLoginService interface {
	Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.UserLogin, uint64, error)
	GetByEmail(ctx context.Context, email string) (*entities.UserLogin, error)
	Create(ctx context.Context, model entities.UserLogin) (uint64, error)
	Update(ctx context.Context, model entities.UserLogin) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}

// IUserGroupService interface
type IUserGroupService interface {
	Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.UserGroup, uint64, error)
	Create(ctx context.Context, model entities.UserGroup) (uint64, error)
	Update(ctx context.Context, model entities.UserGroup) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}

// IUserRoleService interface
type IUserRoleService interface {
	Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.UserRole, uint64, error)
	GetByGroup(ctx context.Context, groupId uint64) ([]*entities.UserRole, error)
	Create(ctx context.Context, model entities.UserRole) (uint64, error)
	Update(ctx context.Context, model entities.UserRole) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}

// IApiEndpointService interface
type IApiEndpointService interface {
	Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiEndpoint, uint64, error)
	Create(ctx context.Context, model entities.ApiEndpoint) (uint64, error)
	Update(ctx context.Context, model entities.ApiEndpoint) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}

// IApiEndpointRbacService interface
type IApiEndpointRbacService interface {
	Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiEndpointRbac, uint64, error)
	Create(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error)
	Update(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
	Validate(ctx context.Context, host string, path string, userRoleId uint64) (*entities.ApiEndpointRbac, error)
}

// IApiLogService interface
type IApiLogService interface {
	Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiLog, uint64, error)
	Create(ctx context.Context, model entities.ApiLog) (uint64, error)
}
