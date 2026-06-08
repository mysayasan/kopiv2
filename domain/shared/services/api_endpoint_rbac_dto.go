package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

type IApiEndpointRbacDtoService[TDto any, TListDto any, TJoinDto any] interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*TListDto, uint64, error)
	GetApiEpByUserRole(ctx context.Context, userId uint64) ([]*TJoinDto, uint64, error)
	Validate(ctx context.Context, host string, path string, userRoleId uint64) (*TDto, error)
	Create(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error)
	Update(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}

type apiEndpointRbacDtoService[TDto any, TListDto any, TJoinDto any] struct {
	shared IApiEndpointRbacService
}

func NewApiEndpointRbacDtoService[TDto any, TListDto any, TJoinDto any](shared IApiEndpointRbacService) IApiEndpointRbacDtoService[TDto, TListDto, TJoinDto] {
	return &apiEndpointRbacDtoService[TDto, TListDto, TJoinDto]{shared: shared}
}

func (m *apiEndpointRbacDtoService[TDto, TListDto, TJoinDto]) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*TListDto, uint64, error) {
	res, totalCnt, err := m.shared.GetList(ctx, limit, offset, filters, sorters)
	return projectSliceResult[TListDto](res, totalCnt, err)
}

func (m *apiEndpointRbacDtoService[TDto, TListDto, TJoinDto]) GetApiEpByUserRole(ctx context.Context, userId uint64) ([]*TJoinDto, uint64, error) {
	res, totalCnt, err := m.shared.GetApiEpByUserRole(ctx, userId)
	return projectSliceResult[TJoinDto](res, totalCnt, err)
}

func (m *apiEndpointRbacDtoService[TDto, TListDto, TJoinDto]) Validate(ctx context.Context, host string, path string, userRoleId uint64) (*TDto, error) {
	res, err := m.shared.Validate(ctx, host, path, userRoleId)
	return projectOne[TDto](res, err)
}

func (m *apiEndpointRbacDtoService[TDto, TListDto, TJoinDto]) Create(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error) {
	return m.shared.Create(ctx, model)
}

func (m *apiEndpointRbacDtoService[TDto, TListDto, TJoinDto]) Update(ctx context.Context, model entities.ApiEndpointRbac) (uint64, error) {
	return m.shared.Update(ctx, model)
}

func (m *apiEndpointRbacDtoService[TDto, TListDto, TJoinDto]) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.shared.Delete(ctx, id)
}
