package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

type IUserGroupDtoService[TDto any] interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*TDto, uint64, error)
	Create(ctx context.Context, model entities.UserGroup) (uint64, error)
	Update(ctx context.Context, model entities.UserGroup) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}

type userGroupDtoService[TDto any] struct {
	shared IUserGroupService
}

func NewUserGroupDtoService[TDto any](shared IUserGroupService) IUserGroupDtoService[TDto] {
	return &userGroupDtoService[TDto]{shared: shared}
}

func (m *userGroupDtoService[TDto]) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*TDto, uint64, error) {
	res, totalCnt, err := m.shared.Get(ctx, limit, offset, filters, sorters)
	return projectSliceResult[TDto](res, totalCnt, err)
}

func (m *userGroupDtoService[TDto]) Create(ctx context.Context, model entities.UserGroup) (uint64, error) {
	return m.shared.Create(ctx, model)
}

func (m *userGroupDtoService[TDto]) Update(ctx context.Context, model entities.UserGroup) (uint64, error) {
	return m.shared.Update(ctx, model)
}

func (m *userGroupDtoService[TDto]) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.shared.Delete(ctx, id)
}
