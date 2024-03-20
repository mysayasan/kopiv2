package repos

import (
	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	"github.com/mysayasan/kopiv2/infra/db/sql/postgres"
)

// homeRepo struct
type homeRepo struct {
	dbCrud postgres.IDbCrud
}

// Create new IHomeRepo
func NewHomeRepo(dbCrud postgres.IDbCrud) IHomeRepo {
	return &homeRepo{
		dbCrud: dbCrud,
	}
}

func (m *homeRepo) GetLatest(limit uint64, offset uint64) ([]*models.ResidentPropListModel, uint64, error) {
	res, totalCnt, err := m.dbCrud.Get(models.ResidentPropListModel{}, "resident_prop", limit, offset)
	if err != nil {
		return nil, 0, err
	}

	list := make([]*models.ResidentPropListModel, 0)

	for _, row := range res {
		var model models.ResidentPropListModel
		mapstructure.Decode(row, &model)
		list = append(list, &model)
	}

	return list, totalCnt, nil
}
