package repos

import (
	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	"github.com/mysayasan/kopiv2/apps/mypropsan/entity"
	"github.com/mysayasan/kopiv2/apps/mypropsan/models"
	"github.com/mysayasan/kopiv2/infra/db/sql/postgres"
)

// uploadRepo struct
type uploadRepo struct {
	dbCrud postgres.IDbCrud
}

// Create new IUploadRepo
func NewUploadRepo(dbCrud postgres.IDbCrud) IUploadRepo {
	return &uploadRepo{
		dbCrud: dbCrud,
	}
}

func (m *uploadRepo) GetByGuid(guid string) (*entity.UploadEntity, error) {
	res, err := m.dbCrud.GetSingle(models.ResidentPropModel{}, "")
	if err != nil {
		return nil, err
	}
	var model *entity.UploadEntity
	mapstructure.Decode(res, model)

	return model, nil
}
