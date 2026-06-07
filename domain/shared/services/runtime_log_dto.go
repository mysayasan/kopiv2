package services

import "context"

type IRuntimeLogDtoService[TDto any] interface {
	List(ctx context.Context, limit uint64, offset uint64) ([]*TDto, uint64, error)
	DeleteByMonth(ctx context.Context, year int, month int) (uint64, error)
	DeleteOlderThan(ctx context.Context, maxRetentionDays int) (uint64, error)
}

type runtimeLogDtoService[TDto any] struct {
	shared IRuntimeLogService
}

func NewRuntimeLogDtoService[TDto any](shared IRuntimeLogService) IRuntimeLogDtoService[TDto] {
	return &runtimeLogDtoService[TDto]{shared: shared}
}

func (m *runtimeLogDtoService[TDto]) List(ctx context.Context, limit uint64, offset uint64) ([]*TDto, uint64, error) {
	res, totalCnt, err := m.shared.List(ctx, limit, offset)
	return projectSliceResult[TDto](res, totalCnt, err)
}

func (m *runtimeLogDtoService[TDto]) DeleteByMonth(ctx context.Context, year int, month int) (uint64, error) {
	return m.shared.DeleteByMonth(ctx, year, month)
}

func (m *runtimeLogDtoService[TDto]) DeleteOlderThan(ctx context.Context, maxRetentionDays int) (uint64, error) {
	return m.shared.DeleteOlderThan(ctx, maxRetentionDays)
}
