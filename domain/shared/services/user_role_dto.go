package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

type IUserRoleDtoService[TDto any] interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*TDto, uint64, error)
	GetByGroup(ctx context.Context, groupId uint64) ([]*TDto, error)
	Create(ctx context.Context, model entities.UserRole) (uint64, error)
	Update(ctx context.Context, model entities.UserRole) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}

type userRoleDtoService[TDto any] struct {
	shared IUserRoleService
}

func NewUserRoleDtoService[TDto any](shared IUserRoleService) IUserRoleDtoService[TDto] {
	return &userRoleDtoService[TDto]{shared: shared}
}

func (m *userRoleDtoService[TDto]) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*TDto, uint64, error) {
	res, totalCnt, err := m.shared.Get(ctx, limit, offset, filters, sorters)
	return projectSliceResult[TDto](res, totalCnt, err)
}

func (m *userRoleDtoService[TDto]) GetByGroup(ctx context.Context, groupId uint64) ([]*TDto, error) {
	res, err := m.shared.GetByGroup(ctx, groupId)
	return projectSlice[TDto](res, err)
}

func (m *userRoleDtoService[TDto]) Create(ctx context.Context, model entities.UserRole) (uint64, error) {
	return m.shared.Create(ctx, model)
}

func (m *userRoleDtoService[TDto]) Update(ctx context.Context, model entities.UserRole) (uint64, error) {
	return m.shared.Update(ctx, model)
}

func (m *userRoleDtoService[TDto]) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.shared.Delete(ctx, id)
}
