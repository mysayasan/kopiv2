package services

import (
	"context"

	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
)

type IApiLogDtoService[TDto any] interface {
	Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*TDto, uint64, error)
	Create(ctx context.Context, model entities.ApiLog) (uint64, error)
	DeleteByMonth(ctx context.Context, year int, month int) (uint64, error)
	DeleteOlderThan(ctx context.Context, maxRetentionDays int) (uint64, error)
}

type apiLogDtoService[TDto any] struct {
	shared IApiLogService
}

func NewApiLogDtoService[TDto any](shared IApiLogService) IApiLogDtoService[TDto] {
	return &apiLogDtoService[TDto]{shared: shared}
}

func (m *apiLogDtoService[TDto]) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*TDto, uint64, error) {
	res, totalCnt, err := m.shared.Get(ctx, limit, offset, filters, sorters)
	return projectSliceResult[TDto](res, totalCnt, err)
}

func (m *apiLogDtoService[TDto]) Create(ctx context.Context, model entities.ApiLog) (uint64, error) {
	return m.shared.Create(ctx, model)
}

func (m *apiLogDtoService[TDto]) DeleteByMonth(ctx context.Context, year int, month int) (uint64, error) {
	return m.shared.DeleteByMonth(ctx, year, month)
}

func (m *apiLogDtoService[TDto]) DeleteOlderThan(ctx context.Context, maxRetentionDays int) (uint64, error) {
	return m.shared.DeleteOlderThan(ctx, maxRetentionDays)
}
