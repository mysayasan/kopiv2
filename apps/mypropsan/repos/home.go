package repos

import (
	"reflect"

	"github.com/gofiber/fiber/v2/log"
	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	"github.com/mysayasan/kopiv2/infra/db/postgres"
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

func (m *homeRepo) GetLatest() ([]*models.ResidentPropListModel, uint64, error) {
	el := reflect.ValueOf(&models.ResidentPropListModel{}).Elem()
	res, cnt, err := m.dbCrud.Get(el, "resident_prop")
	if err != nil {
		return nil, 0, err
	}

	list := make([]*models.ResidentPropListModel, 0)

	for _, row := range res {
		var model models.ResidentPropListModel
		mapstructure.Decode(row, &model)
		list = append(list, &model)
	}

	log.Info(list)
	log.Info(cnt)

	return list, 0, nil
}
