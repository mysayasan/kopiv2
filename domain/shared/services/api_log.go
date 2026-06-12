package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/cache"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

var ErrCurrentMonthApiLogDelete = errors.New("current month API logs cannot be deleted")

// apiLogService struct
type apiLogService struct {
	repo  dbsql.IGenericRepo[entities.ApiLog]
	cache cache.Store
}

// Create new IApiLogService
func NewApiLogService(
	repo dbsql.IGenericRepo[entities.ApiLog],
	cacheStore cache.Store,
) IApiLogService {
	return &apiLogService{
		repo:  repo,
		cache: cacheStore,
	}
}

func (m *apiLogService) Get(ctx context.Context, limit uint64, offset uint64, filters []sqldataenums.Filter, sorters []sqldataenums.Sorter) ([]*entities.ApiLog, uint64, error) {
	if len(sorters) == 0 {
		sorters = []sqldataenums.Sorter{
			{
				FieldName: "CreatedAt",
				Sort:      sqldataenums.DESC,
			},
		}
	}

	return m.repo.Get(ctx, "", limit, offset, filters, sorters)
}

func (m *apiLogService) Create(ctx context.Context, model entities.ApiLog) (uint64, error) {
	return m.repo.Create(ctx, "", model)
}

func (m *apiLogService) DeleteByMonth(ctx context.Context, year int, month int) (uint64, error) {
	if year <= 0 || month < 1 || month > 12 {
		return 0, fmt.Errorf("invalid year or month")
	}

	now := time.Now().UTC()
	if year == now.Year() && month == int(now.Month()) {
		return 0, ErrCurrentMonthApiLogDelete
	}

	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	return m.repo.Delete(ctx, "", []sqldataenums.Filter{
		{
			FieldName: "CreatedAt",
			Compare:   sqldataenums.GreaterThanOrEqualTo,
			Value:     start.Unix(),
		},
		{
			FieldName: "CreatedAt",
			Compare:   sqldataenums.LessThan,
			Value:     end.Unix(),
		},
	})
}

func (m *apiLogService) DeleteOlderThan(ctx context.Context, maxRetentionDays int) (uint64, error) {
	if maxRetentionDays <= 0 {
		return 0, fmt.Errorf("max retention days must be greater than zero")
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -maxRetentionDays)
	return m.repo.Delete(ctx, "", []sqldataenums.Filter{
		{
			FieldName: "CreatedAt",
			Compare:   sqldataenums.LessThan,
			Value:     cutoff.Unix(),
		},
	})
}
