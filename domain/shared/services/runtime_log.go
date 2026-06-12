package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	applog "github.com/mysayasan/kopiv2/infra/logging"
)

var ErrCurrentMonthLogDelete = errors.New("current month runtime logs cannot be deleted")

type runtimeLogService struct {
	logger applog.Logger
}

// Create new IRuntimeLogService
func NewRuntimeLogService(logger applog.Logger) IRuntimeLogService {
	return &runtimeLogService{logger: logger}
}

func (m *runtimeLogService) List(ctx context.Context, limit uint64, offset uint64) ([]applog.Entry, uint64, error) {
	if m.logger == nil {
		return []applog.Entry{}, 0, nil
	}
	return m.logger.List(ctx, limit, offset)
}

func (m *runtimeLogService) DeleteByMonth(ctx context.Context, year int, month int) (uint64, error) {
	now := time.Now()
	if year == now.Year() && month == int(now.Month()) {
		return 0, ErrCurrentMonthLogDelete
	}
	if m.logger == nil {
		return 0, nil
	}
	return m.logger.DeleteByMonth(ctx, year, month)
}

func (m *runtimeLogService) DeleteOlderThan(ctx context.Context, maxRetentionDays int) (uint64, error) {
	if maxRetentionDays <= 0 {
		return 0, fmt.Errorf("max retention days must be greater than 0")
	}
	if m.logger == nil {
		return 0, nil
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -maxRetentionDays)
	return m.logger.DeleteOlderThan(ctx, cutoff)
}
