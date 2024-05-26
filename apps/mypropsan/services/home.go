package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	"github.com/mysayasan/kopiv2/apps/mypropsan/repos"
	"github.com/mysayasan/kopiv2/domain/enums/data"
)

// homeService struct
type homeService struct {
	repo repos.IResidentPropRepo
}

// Create new IHomeService
func NewHomeService(repo repos.IResidentPropRepo) IHomeService {
	return &homeService{
		repo: repo,
	}
}

func (m *homeService) GetLatest(ctx context.Context, limit uint64, offset uint64) ([]*models.ResidentPropModel, uint64, error) {
	var filters []data.Filter
	filter := data.Filter{
		FieldName: "Id",
		Compare:   1,
		Value:     1,
	}

	filters = append(filters, filter)

	sorters := []data.Sorter{
		{
			FieldName: "LandAreaSize",
			Sort:      2,
		},
	}

	return m.repo.GetLatest(ctx, limit, offset, nil, sorters)
}
