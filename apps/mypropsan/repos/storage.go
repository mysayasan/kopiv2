package repos

import (
	"context"

	_ "github.com/lib/pq"
	"github.com/mitchellh/mapstructure"
	"github.com/mysayasan/kopiv2/apps/mypropsan/entity"
	"github.com/mysayasan/kopiv2/infra/db/sql/postgres"
)

// storageRepo struct
type storageRepo struct {
	dbCrud postgres.IDbCrud
}

// Create new IStorageRepo
func NewStorageRepo(dbCrud postgres.IDbCrud) IStorageRepo {
	return &storageRepo{
		dbCrud: dbCrud,
	}
}

func (m *storageRepo) GetByGuid(ctx context.Context, guid string) (*entity.StorageEntity, error) {
	res, err := m.dbCrud.GetSingle(ctx, entity.StorageEntity{}, "")
	if err != nil {
		return nil, err
	}
	var model *entity.StorageEntity
	mapstructure.Decode(res, model)

	return model, nil
}

func (m *storageRepo) Add(ctx context.Context, model entity.StorageEntity) (uint64, error) {
	res, err := m.dbCrud.Add(ctx, model, "")
	if err != nil {
		return 0, err
	}

	return res, nil
}
