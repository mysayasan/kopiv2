package repos

import (
	"reflect"

	_ "github.com/lib/pq"
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
	m.dbCrud.Get(el, "resident_prop")
	return []*models.ResidentPropListModel{}, 0, nil
}
