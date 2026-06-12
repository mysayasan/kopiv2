package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

type IApiEndpointDtoService[TDto any] interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*TDto, uint64, error)
	Create(ctx context.Context, model entities.ApiEndpoint) (uint64, error)
	Update(ctx context.Context, model entities.ApiEndpoint) (uint64, error)
	Delete(ctx context.Context, id uint64) (uint64, error)
}

type apiEndpointDtoService[TDto any] struct {
	shared IApiEndpointService
}

func NewApiEndpointDtoService[TDto any](shared IApiEndpointService) IApiEndpointDtoService[TDto] {
	return &apiEndpointDtoService[TDto]{shared: shared}
}

func (m *apiEndpointDtoService[TDto]) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*TDto, uint64, error) {
	res, totalCnt, err := m.shared.Get(ctx, limit, offset, filters, sorters)
	return projectSliceResult[TDto](res, totalCnt, err)
}

func (m *apiEndpointDtoService[TDto]) Create(ctx context.Context, model entities.ApiEndpoint) (uint64, error) {
	return m.shared.Create(ctx, model)
}

func (m *apiEndpointDtoService[TDto]) Update(ctx context.Context, model entities.ApiEndpoint) (uint64, error) {
	return m.shared.Update(ctx, model)
}

func (m *apiEndpointDtoService[TDto]) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.shared.Delete(ctx, id)
}
