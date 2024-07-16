package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/domain/entities"
	"github.com/mysayasan/kopiv2/domain/enums/data"
	"github.com/mysayasan/kopiv2/domain/shared/repos"
)

// apiLogService struct
type apiLogService struct {
	repo repos.IApiLogRepo
}

// Create new IApiLogService
func NewApiLogService(repo repos.IApiLogRepo) IApiLogService {
	return &apiLogService{
		repo: repo,
	}
}

func (m *apiLogService) GetAll(ctx context.Context, limit uint64, offset uint64) ([]*entities.ApiLogEntity, uint64, error) {
	sorters := []data.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	return m.repo.GetAll(ctx, limit, offset, nil, sorters)
}

func (m *apiLogService) Create(ctx context.Context, model entities.ApiLogEntity) (uint64, error) {
	return m.repo.Create(ctx, model)
}
