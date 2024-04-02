package repos

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	"github.com/mysayasan/kopiv2/infra/db/sql/postgres"
)

// residentPropRepo struct
type residentPropRepo struct {
	dbCrud postgres.IDbCrud
}

// Create new IResidentPropRepo
func NewResidentPropRepo(dbCrud postgres.IDbCrud) IResidentPropRepo {
	return &residentPropRepo{
		dbCrud: dbCrud,
	}
}

func (m *residentPropRepo) GetLatest(ctx context.Context, limit uint64, offset uint64, filters []dbsql.Filter, sorter []dbsql.Sorter) ([]*models.ResidentPropModel, uint64, error) {
	res, totalCnt, err := m.dbCrud.Get(ctx, models.ResidentPropModel{}, limit, offset, filters, sorter, "")
	if err != nil {
		return nil, 0, err
	}

	list := make([]*models.ResidentPropModel, 0)

	for _, row := range res {
		var model models.ResidentPropModel
		mapstructure.Decode(row, &model)
		list = append(list, &model)
	}

	return list, totalCnt, nil
}
