package services

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

// homeService struct
type homeService struct {
	repo dbsql.IGenericRepo[models.ResidentProp]
}

// Create new IHomeService
func NewHomeService(repo dbsql.IGenericRepo[models.ResidentProp]) IHomeService {
	return &homeService{
		repo: repo,
	}
}

func (m *homeService) GetLatest(ctx context.Context, limit uint64, offset uint64) ([]*models.ResidentProp, uint64, error) {
	sorters := []sqldataenums.Sorter{
		{
			FieldName: "LandAreaSize",
			Sort:      2,
		},
	}

	return m.repo.Get(ctx, "", limit, offset, nil, sorters)
}
