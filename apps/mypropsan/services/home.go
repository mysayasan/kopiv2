package services

import (
	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	"github.com/mysayasan/kopiv2/apps/mypropsan/repos"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
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

func (m *homeService) GetLatest(limit uint64, offset uint64) ([]*models.ResidentPropViewModel, uint64, error) {
	var filters []dbsql.Filter
	filter := dbsql.Filter{
		FieldIdx: 0,
		Compare:  1,
		Value:    1,
	}

	filters = append(filters, filter)

	sorters := []dbsql.Sorter{
		{
			FieldIdx: 4,
			Sort:     2,
		},
		{
			FieldIdx: 1,
			Sort:     1,
		},
	}

	return m.repo.GetLatest(limit, offset, nil, sorters)
}
